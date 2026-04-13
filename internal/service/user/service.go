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
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidSession     = errors.New("invalid session")
	ErrInvalidProfile     = errors.New("at least one profile field is required")
)

type Service struct {
	repo     *userrepo.Repository
	sessions *cache.SessionStore
	tokens   *jwtutil.Manager
}

type AuthResult struct {
	User      *model.User `json:"user"`
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Avatar   string `json:"avatar"`
	Profile  string `json:"profile"`
}

type UpdateProfileRequest struct {
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
	Profile  string `json:"profile"`
}

func New(repo *userrepo.Repository, sessionStore *cache.SessionStore, tokens *jwtutil.Manager) *Service {
	return &Service{
		repo:     repo,
		sessions: sessionStore,
		tokens:   tokens,
	}
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
		Username: username,
		Password: string(passwordHash),
		Avatar:   strings.TrimSpace(req.Avatar),
		Profile:  strings.TrimSpace(req.Profile),
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

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
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

func (s *Service) UpdateProfile(ctx context.Context, userID int64, req UpdateProfileRequest) (*model.User, error) {
	updates := make(map[string]any)

	if username := strings.TrimSpace(req.Username); username != "" {
		updates["username"] = username
	}
	if avatar := strings.TrimSpace(req.Avatar); avatar != "" {
		updates["avatar"] = avatar
	}
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		updates["profile"] = profile
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

func sanitizeUser(user *model.User) *model.User {
	if user == nil {
		return nil
	}

	sanitized := *user
	sanitized.Password = ""
	return &sanitized
}
