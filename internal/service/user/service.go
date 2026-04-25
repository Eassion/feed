package usersvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"feed/internal/cache"
	"feed/internal/model"
	userrepo "feed/internal/repository/user"
	"feed/pkg/jwtutil"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrUsernameTaken       = errors.New("username already exists")
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidSession      = errors.New("invalid session")
	ErrInvalidProfile      = errors.New("at least one profile field is required")
	ErrHomepageUnavailable = errors.New("user homepage dependencies are not configured")
)

type Service struct {
	repo         *userrepo.Repository
	sessions     *cache.SessionStore
	tokens       *jwtutil.Manager
	contentStats homepageContentStatsProvider
	userCounts   homepageUserCountProvider
	follows      homepageFollowProvider
	profileCache *cache.CountCacheStore
}

type AuthResult struct {
	User      *model.User `json:"user"`
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	Mobile   string `json:"mobile"`
	Email    string `json:"email"`
}

type UpdateProfileRequest struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	Mobile   string `json:"mobile"`
	Email    string `json:"email"`
}

type homepageContentStatsProvider interface {
	CountPublicPublishedByAuthor(ctx context.Context, authorID int64) (int64, error)
}

type homepageUserCountProvider interface {
	GetUserCounter(ctx context.Context, userID int64) (*model.UserCount, error)
}

type homepageFollowProvider interface {
	IsFollowing(ctx context.Context, followerID, followeeID int64) (bool, error)
}

func New(repo *userrepo.Repository, sessionStore *cache.SessionStore, tokens *jwtutil.Manager) *Service {
	return &Service{
		repo:     repo,
		sessions: sessionStore,
		tokens:   tokens,
	}
}

func (s *Service) SetHomepageProviders(
	contentStats homepageContentStatsProvider,
	userCounts homepageUserCountProvider,
	follows homepageFollowProvider,
	profileCache *cache.CountCacheStore,
) {
	s.contentStats = contentStats
	s.userCounts = userCounts
	s.follows = follows
	s.profileCache = profileCache
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*AuthResult, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		return nil, ErrInvalidCredentials
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Username:     username,
		Nickname:     strings.TrimSpace(req.Nickname),
		PasswordHash: string(passwordHash),
		Avatar:       strings.TrimSpace(req.Avatar),
		Bio:          strings.TrimSpace(req.Bio),
		Mobile:       stringPtrOrNil(req.Mobile),
		Email:        stringPtrOrNil(req.Email),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		if userrepo.IsDuplicateUsername(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}

	return s.createSession(ctx, user)
}

func (s *Service) Login(ctx context.Context, username, password string) (*AuthResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	user, err := s.repo.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return s.createSession(ctx, user)
}

func (s *Service) Logout(ctx context.Context, token string, userID int64) error {
	if token == "" || userID <= 0 {
		return ErrInvalidSession
	}

	return s.sessions.Delete(ctx, token, userID)
}

func (s *Service) GetProfile(ctx context.Context, userID int64) (*model.User, error) {
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return sanitizeUser(user), nil
}

func (s *Service) GetHomepage(ctx context.Context, targetUserID, viewerID int64) (*model.UserHomepage, error) {
	if targetUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s.contentStats == nil || s.userCounts == nil || s.follows == nil {
		return nil, ErrHomepageUnavailable
	}

	user, err := s.GetProfile(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	worksCount, err := s.contentStats.CountPublicPublishedByAuthor(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	counter, err := s.getHomepageCounter(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	isFollowing := false
	if viewerID > 0 && viewerID != targetUserID {
		isFollowing, err = s.follows.IsFollowing(ctx, viewerID, targetUserID)
		if err != nil {
			return nil, err
		}
	}

	return &model.UserHomepage{
		ID:                     user.ID,
		Username:               user.Username,
		Nickname:               user.Nickname,
		Avatar:                 user.Avatar,
		Bio:                    user.Bio,
		WorksCount:             worksCount,
		TotalLikesReceived:     counter.TotalLikesReceived,
		TotalFavoritesReceived: counter.TotalFavoritesReceived,
		FollowersCount:         counter.FollowersCount,
		FollowingCount:         counter.FollowingCount,
		IsFollowing:            isFollowing,
	}, nil
}

func (s *Service) UpdateProfile(ctx context.Context, userID int64, req UpdateProfileRequest) (*model.User, error) {
	updates := make(map[string]any)

	if username := strings.TrimSpace(req.Username); username != "" {
		updates["username"] = username
	}
	if nickname := strings.TrimSpace(req.Nickname); nickname != "" {
		updates["nickname"] = nickname
	}
	if avatar := strings.TrimSpace(req.Avatar); avatar != "" {
		updates["avatar"] = avatar
	}
	if bio := strings.TrimSpace(req.Bio); bio != "" {
		updates["bio"] = bio
	}
	if mobile := stringPtrOrNil(req.Mobile); mobile != nil {
		updates["mobile"] = mobile
	}
	if email := stringPtrOrNil(req.Email); email != nil {
		updates["email"] = email
	}

	if len(updates) == 0 {
		return nil, ErrInvalidProfile
	}

	if err := s.repo.UpdateProfile(ctx, userID, updates); err != nil {
		if userrepo.IsDuplicateUsername(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}

	return s.GetProfile(ctx, userID)
}

// 根据一批用户ID获取对应的用户信息，返回一个以用户ID为键的映射
func (s *Service) BatchGetUserMap(ctx context.Context, userIDs []int64) (map[int64]model.UserSummary, error) {
	users, err := s.repo.BatchFindByIDs(ctx, userIDs)
	if err != nil {
		if errors.Is(err, userrepo.ErrRepositoryUnavailable) {
			return map[int64]model.UserSummary{}, nil
		}
		return nil, err
	}

	result := make(map[int64]model.UserSummary, len(users))
	for _, user := range users {
		result[user.ID] = model.UserSummary{
			ID:       user.ID,
			Username: user.Username,
			Nickname: user.Nickname,
			Avatar:   user.Avatar,
		}
	}

	return result, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (*jwtutil.Claims, error) {
	claims, err := s.tokens.Parse(token)
	if err != nil {
		return nil, err
	}

	ok, err := s.sessions.VerifyAndRenew(ctx, token, claims.UserID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidSession
	}

	return claims, nil
}

func (s *Service) getHomepageCounter(ctx context.Context, targetUserID int64) (*model.UserCount, error) {
	if counter, ok, err := s.profileCache.GetUserProfileCount(ctx, targetUserID); err != nil {
		return nil, err
	} else if ok {
		return counter, nil
	}

	locked, err := s.profileCache.TryAcquireUserProfileRebuildLock(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if locked {
		defer func() {
			_ = s.profileCache.ReleaseUserProfileRebuildLock(ctx, targetUserID)
		}()
	}

	counter, err := s.userCounts.GetUserCounter(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if counter == nil {
		counter = &model.UserCount{UserID: targetUserID}
	}
	counter.UserID = targetUserID
	_ = s.profileCache.SetUserProfileCount(ctx, *counter)
	return counter, nil
}

func (s *Service) createSession(ctx context.Context, user *model.User) (*AuthResult, error) {
	token, expiresAt, err := s.tokens.Generate(user.ID, user.Username)
	if err != nil {
		return nil, err
	}

	if err := s.sessions.Save(ctx, token, user.ID); err != nil {
		return nil, err
	}

	return &AuthResult{
		User:      sanitizeUser(user),
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

// 返回给前端之前，清除掉敏感信息
func sanitizeUser(user *model.User) *model.User {
	if user == nil {
		return nil
	}

	sanitized := *user
	sanitized.PasswordHash = ""
	sanitized.PasswordSalt = ""
	return &sanitized
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
