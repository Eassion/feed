package cache

import (
	"context"
	_ "embed"
	"strconv"
	"time"

	"feed/internal/model"
	"github.com/redis/go-redis/v9"
)

const (
	contentLikeField           = "like_count"
	contentFavoriteField       = "favorite_count"
	contentCommentField        = "comment_count"
	userLikesReceivedField     = "total_likes_received"
	userFavoritesReceivedField = "total_favorites_received"
	userFollowersField         = "followers_count"
	userFollowingField         = "following_count"
)

//go:embed lua/merge_counter_hash.lua
var mergeCounterHashLua string

var mergeCounterHashScript = redis.NewScript(mergeCounterHashLua)

type CountCacheStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewCountCacheStore(client *redis.Client, ttl time.Duration) *CountCacheStore {
	return &CountCacheStore{
		client: client,
		ttl:    ttl,
	}
}

func (s *CountCacheStore) GetContentCount(ctx context.Context, contentID int64) (*model.ContentCount, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, nil
	}

	//查content_id的like_count, favorite_count, comment_count
	values, err := s.client.MGet(ctx,
		//count:value:%d:%d:%d
		CountValueKey(model.CountBizTypeLike, model.CountTargetTypeContent, contentID),
		CountValueKey(model.CountBizTypeFavorite, model.CountTargetTypeContent, contentID),
		CountValueKey(model.CountBizTypeComment, model.CountTargetTypeContent, contentID),
	).Result()
	if err != nil {
		return nil, false, err
	}

	counter := &model.ContentCount{ContentID: contentID}
	hit := assignInt64(values[0], &counter.LikeCount)
	hit = assignInt64(values[1], &counter.FavoriteCount) || hit
	hit = assignInt64(values[2], &counter.CommentCount) || hit

	//三个值里面只要有一个命中就代表这次缓存命中了
	if !hit {
		return nil, false, nil
	}

	return counter, true, nil
}

//批量获取内容统计数据，优先从缓存获取，缺失的部分再从数据库获取，并更新缓存
func (s *CountCacheStore) BatchGetContentCounts(ctx context.Context, contentIDs []int64) (map[int64]model.ContentCount, []int64, error) {
	result := make(map[int64]model.ContentCount, len(contentIDs))
	if len(contentIDs) == 0 {
		return result, nil, nil
	}
	if s == nil || s.client == nil {
		return result, append([]int64(nil), contentIDs...), nil
	}

	pipe := s.client.Pipeline()
	cmds := make(map[int64]*redis.SliceCmd, len(contentIDs))
	//获取content_id的like_count, favorite_count, comment_count
	for _, contentID := range contentIDs {
		cmds[contentID] = pipe.MGet(ctx,
			CountValueKey(model.CountBizTypeLike, model.CountTargetTypeContent, contentID),
			CountValueKey(model.CountBizTypeFavorite, model.CountTargetTypeContent, contentID),
			CountValueKey(model.CountBizTypeComment, model.CountTargetTypeContent, contentID),
		)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, nil, err
	}

	missing := make([]int64, 0)
	for _, contentID := range contentIDs {
		values, err := cmds[contentID].Result()
		if err != nil && err != redis.Nil {
			return nil, nil, err
		}

		counter := model.ContentCount{ContentID: contentID}
		hit := assignInt64(values[0], &counter.LikeCount)
		hit = assignInt64(values[1], &counter.FavoriteCount) || hit
		hit = assignInt64(values[2], &counter.CommentCount) || hit
		//三个值里面只要有一个命中就代表这次缓存命中了
		//没有命中就加入到missing列表，等会从数据库里查询
		if !hit {
			missing = append(missing, contentID)
			continue
		}
		result[contentID] = counter
	}

	return result, missing, nil
}

func (s *CountCacheStore) GetUserCount(ctx context.Context, userID int64) (*model.UserCount, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, nil
	}

	values, err := s.client.MGet(ctx,
		CountValueKey(model.CountBizTypeLike, model.CountTargetTypeUser, userID),
		CountValueKey(model.CountBizTypeFavorite, model.CountTargetTypeUser, userID),
		CountValueKey(model.CountBizTypeFollowed, model.CountTargetTypeUser, userID),
		CountValueKey(model.CountBizTypeFollowing, model.CountTargetTypeUser, userID),
	).Result()
	if err != nil {
		return nil, false, err
	}

	counter := &model.UserCount{UserID: userID}
	hit := assignInt64(values[0], &counter.TotalLikesReceived)
	hit = assignInt64(values[1], &counter.TotalFavoritesReceived) || hit
	hit = assignInt64(values[2], &counter.FollowersCount) || hit
	hit = assignInt64(values[3], &counter.FollowingCount) || hit
	if !hit {
		return nil, false, nil
	}

	return counter, true, nil
}

func (s *CountCacheStore) GetUserProfileCount(ctx context.Context, userID int64) (*model.UserCount, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, nil
	}

	return s.getUserCountByKey(ctx, UserProfileCountKey(userID), userID)
}

func (s *CountCacheStore) getUserCountByKey(ctx context.Context, key string, userID int64) (*model.UserCount, bool, error) {
	values, err := s.client.HMGet(ctx, key, userLikesReceivedField, userFavoritesReceivedField, userFollowersField, userFollowingField).Result()
	if err != nil {
		return nil, false, err
	}

	counter := &model.UserCount{UserID: userID}
	hit := assignInt64(values[0], &counter.TotalLikesReceived)
	hit = assignInt64(values[1], &counter.TotalFavoritesReceived) || hit
	hit = assignInt64(values[2], &counter.FollowersCount) || hit
	hit = assignInt64(values[3], &counter.FollowingCount) || hit
	if !hit {
		return nil, false, nil
	}

	return counter, true, nil
}

func (s *CountCacheStore) SetContentCount(ctx context.Context, counter model.ContentCount) error {
	if s == nil || s.client == nil {
		return nil
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, CountValueKey(model.CountBizTypeLike, model.CountTargetTypeContent, counter.ContentID), counter.LikeCount, s.ttl)
	pipe.Set(ctx, CountValueKey(model.CountBizTypeFavorite, model.CountTargetTypeContent, counter.ContentID), counter.FavoriteCount, s.ttl)
	pipe.Set(ctx, CountValueKey(model.CountBizTypeComment, model.CountTargetTypeContent, counter.ContentID), counter.CommentCount, s.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *CountCacheStore) SetUserCount(ctx context.Context, counter model.UserCount) error {
	if s == nil || s.client == nil {
		return nil
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, CountValueKey(model.CountBizTypeLike, model.CountTargetTypeUser, counter.UserID), counter.TotalLikesReceived, s.ttl)
	pipe.Set(ctx, CountValueKey(model.CountBizTypeFavorite, model.CountTargetTypeUser, counter.UserID), counter.TotalFavoritesReceived, s.ttl)
	pipe.Set(ctx, CountValueKey(model.CountBizTypeFollowed, model.CountTargetTypeUser, counter.UserID), counter.FollowersCount, s.ttl)
	pipe.Set(ctx, CountValueKey(model.CountBizTypeFollowing, model.CountTargetTypeUser, counter.UserID), counter.FollowingCount, s.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *CountCacheStore) SetUserProfileCount(ctx context.Context, counter model.UserCount) error {
	if s == nil || s.client == nil {
		return nil
	}

	return s.setUserCountByKey(ctx, UserProfileCountKey(counter.UserID), counter)
}

func (s *CountCacheStore) setUserCountByKey(ctx context.Context, key string, counter model.UserCount) error {
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, key, map[string]any{
		userLikesReceivedField:     counter.TotalLikesReceived,
		userFavoritesReceivedField: counter.TotalFavoritesReceived,
		userFollowersField:         counter.FollowersCount,
		userFollowingField:         counter.FollowingCount,
	})
	if s.ttl > 0 {
		pipe.Expire(ctx, key, s.ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *CountCacheStore) DeleteContentCount(ctx context.Context, contentID int64) error {
	if s == nil || s.client == nil || contentID <= 0 {
		return nil
	}

	return s.client.Del(ctx,
		CountValueKey(model.CountBizTypeLike, model.CountTargetTypeContent, contentID),
		CountValueKey(model.CountBizTypeFavorite, model.CountTargetTypeContent, contentID),
		CountValueKey(model.CountBizTypeComment, model.CountTargetTypeContent, contentID),
	).Err()
}

func (s *CountCacheStore) DeleteUserCount(ctx context.Context, userID int64) error {
	if s == nil || s.client == nil || userID <= 0 {
		return nil
	}

	return s.client.Del(ctx,
		CountValueKey(model.CountBizTypeLike, model.CountTargetTypeUser, userID),
		CountValueKey(model.CountBizTypeFavorite, model.CountTargetTypeUser, userID),
		CountValueKey(model.CountBizTypeFollowed, model.CountTargetTypeUser, userID),
		CountValueKey(model.CountBizTypeFollowing, model.CountTargetTypeUser, userID),
	).Err()
}

func (s *CountCacheStore) DeleteUserProfileCount(ctx context.Context, userID int64) error {
	if s == nil || s.client == nil || userID <= 0 {
		return nil
	}

	return s.client.Del(ctx, UserProfileCountKey(userID)).Err()
}

func (s *CountCacheStore) TryAcquireUserProfileRebuildLock(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.client == nil || userID <= 0 {
		return false, nil
	}

	return s.client.SetNX(ctx, UserProfileCountLockKey(userID), "1", UserProfileRebuildLockTTL).Result()
}

func (s *CountCacheStore) ReleaseUserProfileRebuildLock(ctx context.Context, userID int64) error {
	if s == nil || s.client == nil || userID <= 0 {
		return nil
	}

	return s.client.Del(ctx, UserProfileCountLockKey(userID)).Err()
}

// 把value解析成int64类型然后赋值给target
func assignInt64(value any, target *int64) bool {
	if target == nil || value == nil {
		return false
	}

	switch typed := value.(type) {
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return false
		}
		*target = parsed
		return true
	case int64:
		*target = typed
		return true
	case int:
		*target = int64(typed)
		return true
	case []byte:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		if err != nil {
			return false
		}
		*target = parsed
		return true
	default:
		return false
	}
}
