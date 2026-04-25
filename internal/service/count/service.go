package countsvc

import (
	"context"
	"errors"

	"feed/internal/cache"
	"feed/internal/model"
	countrepo "feed/internal/repository/count"
)

var ErrInvalidCountQuery = errors.New("invalid count query")

type Service struct {
	repo  *countrepo.Repository
	cache *cache.CountCacheStore
}

func New(repo *countrepo.Repository, cacheStore *cache.CountCacheStore) *Service {
	return &Service{
		repo:  repo,
		cache: cacheStore,
	}
}

func (s *Service) EnsureContentCounter(ctx context.Context, contentID int64) error {
	return s.repo.EnsureContentCounter(ctx, contentID)
}

func (s *Service) EnsureUserCounter(ctx context.Context, userID int64) error {
	return s.repo.EnsureUserCounter(ctx, userID)
}

//常见旁路缓存 先读redis,再读数据库，最后写redis
func (s *Service) GetContentCounter(ctx context.Context, contentID int64) (*model.ContentCount, error) {
	//从countcache里读content_id的like_count, favorite_count, comment_count
	if counter, ok, err := s.cache.GetContentCount(ctx, contentID); err != nil {
		return nil, err
	} else if ok {
		return counter, nil
	}

	//如果countcache里没有命中，就从数据库里读content_id的like_count, favorite_count, comment_count
	//查的也是专门用来计数的count_value表
	counter, err := s.repo.GetContentCounter(ctx, contentID)
	if err != nil {
		if errors.Is(err, countrepo.ErrRepositoryUnavailable) {
			return &model.ContentCount{ContentID: contentID}, nil
		}
		return nil, err
	}

	_ = s.cache.SetContentCount(ctx, *counter)
	return counter, nil
}

func (s *Service) GetUserCounter(ctx context.Context, userID int64) (*model.UserCount, error) {
	if counter, ok, err := s.cache.GetUserCount(ctx, userID); err != nil {
		return nil, err
	} else if ok {
		return counter, nil
	}

	counter, err := s.repo.GetUserCounter(ctx, userID)
	if err != nil {
		if errors.Is(err, countrepo.ErrRepositoryUnavailable) {
			return &model.UserCount{UserID: userID}, nil
		}
		return nil, err
	}

	_ = s.cache.SetUserCount(ctx, *counter)
	return counter, nil
}

//批量获取内容统计数据，优先从缓存获取，缺失的部分再从数据库获取，并更新缓存
func (s *Service) BatchGetContentCountMap(ctx context.Context, contentIDs []int64) (map[int64]model.ContentCount, error) {
	
	//先从缓存获取
	result, missingIDs, err := s.cache.BatchGetContentCounts(ctx, contentIDs)
	if err != nil {
		return nil, err
	}
	if len(missingIDs) == 0 {
		return result, nil
	}

	//从数据库读
	dbResult, err := s.repo.BatchGetContentCounterMap(ctx, missingIDs)
	if err != nil && errors.Is(err, countrepo.ErrRepositoryUnavailable) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	for _, contentID := range missingIDs {
		counter, ok := dbResult[contentID]
		if !ok {
			counter = model.ContentCount{ContentID: contentID}
		}
		result[contentID] = counter
		//miss中的值回填缓存
		_ = s.cache.SetContentCount(ctx, counter)
	}

	return result, nil
}

func (s *Service) AddLike(ctx context.Context, contentID, authorID, delta int64) error {
	return s.ApplyMutation(ctx, countrepo.Mutation{
		ContentID:          contentID,
		LikeDelta:          delta,
		UserID:             authorID,
		LikesReceivedDelta: delta,
	})
}

func (s *Service) AddFavorite(ctx context.Context, contentID, authorID, delta int64) error {
	return s.ApplyMutation(ctx, countrepo.Mutation{
		ContentID:              contentID,
		FavoriteDelta:          delta,
		UserID:                 authorID,
		FavoritesReceivedDelta: delta,
	})
}

func (s *Service) AddComment(ctx context.Context, contentID, delta int64) error {
	return s.ApplyMutation(ctx, countrepo.Mutation{
		ContentID:    contentID,
		CommentDelta: delta,
	})
}

func (s *Service) ApplyMutation(ctx context.Context, mutation countrepo.Mutation) error {
	return s.repo.ApplyMutation(ctx, mutation)
}

func (s *Service) InvalidateContentCounter(ctx context.Context, contentID int64) error {
	return s.cache.DeleteContentCount(ctx, contentID)
}

func (s *Service) InvalidateUserCounter(ctx context.Context, userID int64) error {
	if err := s.cache.DeleteUserCount(ctx, userID); err != nil {
		return err
	}
	return s.cache.DeleteUserProfileCount(ctx, userID)
}

func ZeroContentCount(contentID int64) model.ContentCount {
	return model.ContentCount{ContentID: contentID}
}
