package interactionsvc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feed/internal/cache"
	"feed/internal/model"
	contentrepo "feed/internal/repository/content"
	interactionrepo "feed/internal/repository/interaction"
)

var (
	ErrInvalidInteraction = errors.New("invalid interaction payload")
	ErrCommentNotFound    = interactionrepo.ErrCommentNotFound
	ErrContentNotFound    = contentrepo.ErrContentNotFound
)

type Service struct {
	repo        *interactionrepo.Repository
	contentRepo *contentrepo.Repository
	states      *cache.HashStateStore
	likeStore   *cache.LikeStore
	likeEvents  LikeEventProducer
	likes       LikeSyncer
	follows     FollowSyncer
	favorites   FavoriteSyncer
	comments    CommentSyncer
	logger      *slog.Logger
}

type LikeSyncer interface {
	HandleUserLikeChanged(ctx context.Context, contentID, authorID, delta int64)
}

type LikeEventProducer interface {
	SendLikeEvent(ctx context.Context, event model.LikeEvent) error
	SendCancelLikeEvent(ctx context.Context, event model.LikeEvent) error
}

type FollowSyncer interface {
	HandleUserFollowed(ctx context.Context, followerID, followeeID int64)
}

type FavoriteSyncer interface {
	HandleUserFavoriteChanged(ctx context.Context, userID, contentID, favoriteID int64, active bool)
}

type CommentSyncer interface {
	HandleCommentCreated(ctx context.Context, comment *model.Comment)
	HandleCommentDeleted(ctx context.Context, result *interactionrepo.CommentDeleteResult)
}

type CommentInput struct {
	ContentID     int64
	ParentID      int64
	RootID        int64
	ReplyToUserID int64
	Text          string
}

func New(repo *interactionrepo.Repository, contentRepository *contentrepo.Repository, states *cache.HashStateStore, logger *slog.Logger) *Service {
	return &Service{
		repo:        repo,
		contentRepo: contentRepository,
		states:      states,
		logger:      logger,
	}
}

func (s *Service) SetFollowSyncer(syncer FollowSyncer) {
	s.follows = syncer
}

func (s *Service) SetLikeSyncer(syncer LikeSyncer) {
	s.likes = syncer
}

func (s *Service) SetLikeFlow(store *cache.LikeStore, producer LikeEventProducer) {
	s.likeStore = store
	s.likeEvents = producer
}

func (s *Service) SetFavoriteSyncer(syncer FavoriteSyncer) {
	s.favorites = syncer
}

func (s *Service) SetCommentSyncer(syncer CommentSyncer) {
	s.comments = syncer
}

func (s *Service) Like(ctx context.Context, userID, contentID int64) (*model.ToggleResult, error) {
	return s.LikeWithScene(ctx, userID, contentID, "")
}

func (s *Service) LikeWithScene(ctx context.Context, userID, contentID int64, scene string) (*model.ToggleResult, error) {
	if s.likeStore != nil && s.likeEvents != nil {
		return s.processLikeEvent(ctx, userID, contentID, scene, true)
	}
	return s.toggleContentState(ctx, userID, contentID, cache.LikeUserKey(userID), true,
		func(ctx context.Context, userID, contentID, authorID int64) (bool, error) {
			return s.repo.EnsureLike(ctx, userID, contentID, authorID)
		},
		func(authorID int64) {
			if s.likes != nil {
				s.likes.HandleUserLikeChanged(ctx, contentID, authorID, 1)
			}
		},
	)
}

func (s *Service) Unlike(ctx context.Context, userID, contentID int64) (*model.ToggleResult, error) {
	return s.UnlikeWithScene(ctx, userID, contentID, "")
}

func (s *Service) UnlikeWithScene(ctx context.Context, userID, contentID int64, scene string) (*model.ToggleResult, error) {
	if s.likeStore != nil && s.likeEvents != nil {
		return s.processLikeEvent(ctx, userID, contentID, scene, false)
	}
	return s.toggleContentState(ctx, userID, contentID, cache.LikeUserKey(userID), false,
		func(ctx context.Context, userID, contentID, _ int64) (bool, error) {
			return s.repo.RemoveLike(ctx, userID, contentID)
		},
		func(authorID int64) {
			if s.likes != nil {
				s.likes.HandleUserLikeChanged(ctx, contentID, authorID, -1)
			}
		},
	)
}

func (s *Service) Favorite(ctx context.Context, userID, contentID int64) (*model.ToggleResult, error) {
	if userID <= 0 || contentID <= 0 {
		return nil, ErrInvalidInteraction
	}

	authorID, err := s.contentRepo.GetAuthorID(ctx, contentID)
	if err != nil {
		return nil, err
	}

	changed, err := s.states.SetState(ctx, cache.FavoriteUserKey(userID), contentID, true)
	if err != nil {
		return nil, err
	}
	if !changed {
		return &model.ToggleResult{Changed: false, Active: true}, nil
	}

	favoriteID, changedDB, err := s.repo.EnsureFavorite(ctx, userID, contentID, authorID)
	if err != nil {
		return nil, err
	}
	if changedDB && s.favorites != nil {
		s.favorites.HandleUserFavoriteChanged(ctx, userID, contentID, favoriteID, true)
	}

	return &model.ToggleResult{Changed: changedDB, Active: true}, nil
}

func (s *Service) Unfavorite(ctx context.Context, userID, contentID int64) (*model.ToggleResult, error) {
	if userID <= 0 || contentID <= 0 {
		return nil, ErrInvalidInteraction
	}

	changed, err := s.states.SetState(ctx, cache.FavoriteUserKey(userID), contentID, false)
	if err != nil {
		return nil, err
	}
	if !changed {
		return &model.ToggleResult{Changed: false, Active: false}, nil
	}

	deleted, err := s.repo.RemoveFavorite(ctx, userID, contentID)
	if err != nil {
		return nil, err
	}
	if deleted && s.favorites != nil {
		s.favorites.HandleUserFavoriteChanged(ctx, userID, contentID, 0, false)
	}

	return &model.ToggleResult{Changed: deleted, Active: false}, nil
}

func (s *Service) Comment(ctx context.Context, userID int64, input CommentInput) (*model.Comment, error) {
	text := strings.TrimSpace(input.Text)
	if userID <= 0 || input.ContentID <= 0 || text == "" {
		return nil, ErrInvalidInteraction
	}

	contentUserID, err := s.contentRepo.GetAuthorID(ctx, input.ContentID)
	if err != nil {
		return nil, err
	}

	comment := &model.Comment{
		UserID:        userID,
		ContentID:     input.ContentID,
		ContentUserID: contentUserID,
		ParentID:      0,
		RootID:        0,
		Comment:       text,
	}
	if input.ParentID > 0 {
		parent, err := s.repo.GetActiveComment(ctx, input.ParentID)
		if err != nil {
			return nil, err
		}
		if parent.ContentID != input.ContentID {
			return nil, ErrInvalidInteraction
		}

		rootID := parent.RootID
		if rootID == 0 {
			rootID = parent.ID
		}
		if input.RootID > 0 && input.RootID != rootID {
			return nil, ErrInvalidInteraction
		}

		replyToUserID := parent.UserID
		if input.ReplyToUserID > 0 && input.ReplyToUserID != replyToUserID {
			return nil, ErrInvalidInteraction
		}

		comment.ParentID = parent.ID
		comment.RootID = rootID
		comment.ReplyToUserID = replyToUserID
	} else if input.RootID > 0 || input.ReplyToUserID > 0 {
		return nil, ErrInvalidInteraction
	}

	//直接落库
	if err := s.repo.CreateComment(ctx, comment); err != nil {
		return nil, err
	}
	//
	if s.comments != nil {
		s.comments.HandleCommentCreated(ctx, comment)
	}

	return comment, nil
}

func (s *Service) DeleteComment(ctx context.Context, userID, commentID int64) error {
	if userID <= 0 || commentID <= 0 {
		return ErrInvalidInteraction
	}

	result, err := s.repo.DeleteCommentByAuthor(ctx, commentID, userID)
	if err != nil {
		return err
	}
	if s.comments != nil {
		s.comments.HandleCommentDeleted(ctx, result)
	}

	return nil
}

func (s *Service) Follow(ctx context.Context, followerID, followeeID int64) (*model.ToggleResult, error) {
	if followerID <= 0 || followeeID <= 0 || followerID == followeeID {
		return nil, ErrInvalidInteraction
	}

	//判断缓存中有没有关注过，如果成功有修改动作，说明之前不存在，changer=1
	changed, err := s.states.SetState(ctx, cache.FollowUserKey(followerID), followeeID, true)
	if err != nil {
		return nil, err
	}
	if !changed {
		//无需修改，Active: true表示最终状态为true，Changed: false表示没有发生修改动作
		//无需修改直接return了，无需执行数据库操作，也无需发送关注事件了
		return &model.ToggleResult{Changed: false, Active: true}, nil
	}

	//执行了修改动作，说明之前不存在，现在要在数据库中创建关注关系了
	//幂等性语句
	created, err := s.repo.EnsureFollow(ctx, followerID, followeeID)
	if err != nil {
		return nil, err
	}
	//只有真正在数据库中插入记录才发送事件，之前已经存在了但状态是false的情况，
	// 虽然也修改了状态变成true了，但是不算是新关注了，不需要发送事件
	if created && s.follows != nil {
		//从缓存/数据库读作品然后填到inbox里面
		s.follows.HandleUserFollowed(ctx, followerID, followeeID)
	}

	return &model.ToggleResult{Changed: created, Active: true}, nil
}

func (s *Service) Unfollow(ctx context.Context, followerID, followeeID int64) (*model.ToggleResult, error) {
	if followerID <= 0 || followeeID <= 0 || followerID == followeeID {
		return nil, ErrInvalidInteraction
	}

	changed, err := s.states.SetState(ctx, cache.FollowUserKey(followerID), followeeID, false)
	if err != nil {
		return nil, err
	}
	if !changed {
		return &model.ToggleResult{Changed: false, Active: false}, nil
	}

	deleted, err := s.repo.RemoveFollow(ctx, followerID, followeeID)
	if err != nil {
		return nil, err
	}

	return &model.ToggleResult{Changed: deleted, Active: false}, nil
}

func (s *Service) ListFollowerIDs(ctx context.Context, followeeID int64) ([]int64, error) {
	if followeeID <= 0 {
		return nil, ErrInvalidInteraction
	}

	result, err := s.repo.ListFollowerIDs(ctx, followeeID)
	if err != nil && errors.Is(err, interactionrepo.ErrRepositoryUnavailable) {
		return []int64{}, nil
	}
	return result, err
}

func (s *Service) ListFolloweeIDs(ctx context.Context, followerID int64) ([]int64, error) {
	if followerID <= 0 {
		return nil, ErrInvalidInteraction
	}

	result, err := s.repo.ListFolloweeIDs(ctx, followerID)
	if err != nil && errors.Is(err, interactionrepo.ErrRepositoryUnavailable) {
		return []int64{}, nil
	}
	return result, err
}

func (s *Service) ListFollowerUserIDsForRebuild(ctx context.Context, afterUserID int64, limit int) ([]int64, error) {
	result, err := s.repo.ListFollowerUserIDsForRebuild(ctx, afterUserID, limit)
	if err != nil && errors.Is(err, interactionrepo.ErrRepositoryUnavailable) {
		return []int64{}, nil
	}
	return result, err
}

func (s *Service) ListFavoriteEntries(ctx context.Context, userID int64, limit int64) ([]model.Favorite, error) {
	if userID <= 0 {
		return nil, ErrInvalidInteraction
	}

	result, err := s.repo.ListFavoriteEntries(ctx, userID, limit)
	if err != nil && errors.Is(err, interactionrepo.ErrRepositoryUnavailable) {
		return []model.Favorite{}, nil
	}
	return result, err
}

// 获取用户对一批content的点赞状态，返回contentID到是否点赞的映射
func (s *Service) BatchQueryLikeInfoMap(ctx context.Context, userID int64, contentIDs []int64) (map[int64]bool, error) {
	if userID <= 0 || len(contentIDs) == 0 {
		return map[int64]bool{}, nil
	}

	//先尝试从缓存查
	if s.likeStore != nil {
		//先从likeStore（即redis）中批量获取点赞状态，如果有任何一个contentID的状态是true（即用户点赞过），就返回结果；
		//如果全部contentID的状态都是false，则可能是缓存未命中或已过期，这时再从数据库查询最新的点赞状态并返回
		result, coldIDs, err := s.likeStore.BatchGetStates(ctx, userID, contentIDs)
		if err == nil && len(coldIDs) == 0 {
			return result, nil
		}
		if err == nil {
			//从数据库查询coldIDs对应的点赞状态，并更新到result中返回
			dbResult, dbErr := s.repo.BatchGetLikeMap(ctx, userID, coldIDs)
			if dbErr != nil && errors.Is(dbErr, interactionrepo.ErrRepositoryUnavailable) {
				return result, nil
			}
			if dbErr != nil {
				return nil, dbErr
			}
			for _, contentID := range coldIDs {
				result[contentID] = dbResult[contentID]
			}
			return result, nil
		}
	}

	// 旧的通用缓存，暂时作为一个兜底                 like:user:{userID}
	if stateMap, err := s.states.BatchGetStates(ctx, cache.LikeUserKey(userID), contentIDs); err == nil {
		hasAny := false
		for _, liked := range stateMap {
			if liked {
				hasAny = true
				break
			}
		}
		if hasAny {
			return stateMap, nil
		}
	}

	//从数据库查，只要存在记录就是点赞过
	result, err := s.repo.BatchGetLikeMap(ctx, userID, contentIDs)
	if err != nil && errors.Is(err, interactionrepo.ErrRepositoryUnavailable) {
		return map[int64]bool{}, nil
	}
	return result, err
}

// 获取用户对一批content的收藏状态，返回contentID到是否收藏的映射
func (s *Service) BatchQueryFavoriteInfoMap(ctx context.Context, userID int64, contentIDs []int64) (map[int64]bool, error) {
	if userID <= 0 || len(contentIDs) == 0 {
		return map[int64]bool{}, nil
	}

	//从缓存读是否收藏了该视频
	if stateMap, err := s.states.BatchGetStates(ctx, cache.FavoriteUserKey(userID), contentIDs); err == nil {
		hasAny := false
		for _, favorited := range stateMap {
			if favorited {
				hasAny = true
				break
			}
		}
		//如果全为false，则可能是缓存未命中或已过期，这时再从数据库查询最新的收藏状态并返回；
		//如果有任何一个contentID的状态是true（即用户收藏过），就直接返回结果；
		if hasAny {
			return stateMap, nil
		}
	}

	//从数据库读  有这条记录说明收藏过，没有记录说明未收藏
	result, err := s.repo.BatchGetFavoriteMap(ctx, userID, contentIDs)
	if err != nil && errors.Is(err, interactionrepo.ErrRepositoryUnavailable) {
		return map[int64]bool{}, nil
	}
	return result, err
}

//
func (s *Service) IsFollowing(ctx context.Context, followerID, followeeID int64) (bool, error) {
	if followerID <= 0 || followeeID <= 0 || followerID == followeeID {
		return false, nil
	}

	//先从缓存读是否关注了该用户，如果缓存中有且是true，则直接返回true；如果缓存中有且是false，则直接返回false；如果缓存中没有，则从数据库查询并返回结果
	stateMap, err := s.states.BatchGetStates(ctx, cache.FollowUserKey(followerID), []int64{followeeID})
	if err == nil && stateMap[followeeID] {
		return true, nil
	}

	result, err := s.repo.IsFollowing(ctx, followerID, followeeID)
	if err != nil && errors.Is(err, interactionrepo.ErrRepositoryUnavailable) {
		return false, nil
	}
	return result, err
}

func (s *Service) toggleContentState(
	ctx context.Context,
	userID, contentID int64,
	key string,
	active bool,
	dbAction func(context.Context, int64, int64, int64) (bool, error),
	onChanged func(authorID int64),
) (*model.ToggleResult, error) {
	if userID <= 0 || contentID <= 0 {
		return nil, ErrInvalidInteraction
	}

	authorID, err := s.contentRepo.GetAuthorID(ctx, contentID)
	if err != nil {
		return nil, err
	}

	changed, err := s.states.SetState(ctx, key, contentID, active)
	if err != nil {
		return nil, err
	}
	if !changed {
		return &model.ToggleResult{Changed: false, Active: active}, nil
	}

	changedDB, err := dbAction(ctx, userID, contentID, authorID)
	if err != nil {
		return nil, err
	}
	if changedDB {
		onChanged(authorID)
	}

	return &model.ToggleResult{Changed: changedDB, Active: active}, nil
}

func (s *Service) processLikeEvent(ctx context.Context, userID, contentID int64, scene string, active bool) (*model.ToggleResult, error) {
	if userID <= 0 || contentID <= 0 {
		return nil, ErrInvalidInteraction
	}

	authorID, err := s.contentRepo.GetAuthorID(ctx, contentID)
	if err != nil {
		return nil, err
	}

	var changed bool
	if active {
		changed, err = s.likeStore.ProcessLike(ctx, userID, contentID)
	} else {
		changed, err = s.likeStore.ProcessUnlike(ctx, userID, contentID)
	}
	if err != nil {
		return nil, err
	}
	if !changed {
		return &model.ToggleResult{Changed: false, Active: active}, nil
	}

	event := model.LikeEvent{
		EventID:       fmt.Sprintf("like:%d:%d:%d", userID, contentID, time.Now().UnixNano()),
		UserID:        userID,
		ContentID:     contentID,
		ContentUserID: authorID,
		Scene:         strings.TrimSpace(scene),
		Timestamp:     time.Now(),
	}

	if active {
		err = s.likeEvents.SendLikeEvent(ctx, event)
	} else {
		err = s.likeEvents.SendCancelLikeEvent(ctx, event)
	}
	if err != nil {
		return nil, err
	}

	return &model.ToggleResult{Changed: true, Active: active}, nil
}
