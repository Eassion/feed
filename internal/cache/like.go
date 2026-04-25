package cache

import (
	"context"
	_ "embed"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	RedisLikeUserHashCapacity = int64(10000)
	RedisLikeExpireSeconds    = int64(5 * 24 * 60 * 60)
)

const (
	likeMetaMinCID   = "_mincid"
	likeMetaExpireAt = "_expire_at"
	likeMetaVer      = "_ver"
)

//go:embed lua/set_like_user_hash.lua
var setLikeUserHashLua string

//go:embed lua/cancel_like_user_hash.lua
var cancelLikeUserHashLua string

var (
	setLikeUserHashScript    = redis.NewScript(setLikeUserHashLua)
	cancelLikeUserHashScript = redis.NewScript(cancelLikeUserHashLua)
)

type LikeStore struct {
	client   *redis.Client
	capacity int64
	ttl      time.Duration
}

func NewLikeStore(client *redis.Client) *LikeStore {
	return &LikeStore{
		client:   client,
		capacity: RedisLikeUserHashCapacity,
		ttl:      time.Duration(RedisLikeExpireSeconds) * time.Second,
	}
}

func (s *LikeStore) ProcessLike(ctx context.Context, userID, contentID int64) (bool, error) {
	if s == nil || s.client == nil {
		return true, nil
	}
	if userID <= 0 || contentID <= 0 {
		return false, nil
	}

	result, err := setLikeUserHashScript.Run(
		ctx,
		s.client,
		[]string{LikeUserKey(userID)},
		strconv.FormatInt(contentID, 10),
		strconv.FormatInt(time.Now().Unix(), 10),
		strconv.FormatInt(int64(s.ttl/time.Second), 10),
		strconv.FormatInt(s.capacity, 10),
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (s *LikeStore) ProcessUnlike(ctx context.Context, userID, contentID int64) (bool, error) {
	if s == nil || s.client == nil {
		return true, nil
	}
	if userID <= 0 || contentID <= 0 {
		return false, nil
	}

	result, err := cancelLikeUserHashScript.Run(
		ctx,
		s.client,
		[]string{LikeUserKey(userID)},
		strconv.FormatInt(contentID, 10),
		strconv.FormatInt(time.Now().Unix(), 10),
		strconv.FormatInt(int64(s.ttl/time.Second), 10),
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// 返回用户对一批content的点赞状态，返回contentID到是否点赞的映射，以及需要从数据库查询的contentID列表（即缓存中没有且可能已经过期的contentID）
func (s *LikeStore) BatchGetStates(ctx context.Context, userID int64, contentIDs []int64) (map[int64]bool, []int64, error) {
	result := make(map[int64]bool, len(contentIDs))
	if userID <= 0 || len(contentIDs) == 0 {
		return result, nil, nil
	}
	if s == nil || s.client == nil {
		return result, append([]int64(nil), contentIDs...), nil
	}

	//like:user:{userID}
	key := LikeUserKey(userID)
	fields := make([]string, 0, len(contentIDs))
	for _, contentID := range contentIDs {
		fields = append(fields, strconv.FormatInt(contentID, 10))
	}

	pipe := s.client.Pipeline()
	minCIDCmd := pipe.HGet(ctx, key, likeMetaMinCID)
	valuesCmd := pipe.HMGet(ctx, key, fields...)
	//每次读点赞缓存时都刷新过期时间，默认5天
	pipe.HSet(ctx, key, likeMetaExpireAt, strconv.FormatInt(time.Now().Add(s.ttl).Unix(), 10))
	pipe.Expire(ctx, key, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, nil, err
	}

	minCID, _ := minCIDCmd.Int64()
	values, err := valuesCmd.Result()
	if err != nil {
		return nil, nil, err
	}

	coldIDs := make([]int64, 0)
	for idx, value := range values {
		contentID := contentIDs[idx]
		if value != nil {
			//如果缓存中有这个contentID的点赞状态，且状态是"1"，则说明用户点赞过，直接返回true；如果状态不是"1"，则说明用户未点赞过，直接返回false
			if str, ok := value.(string); ok && str == "1" {
				result[contentID] = true
				continue
			}
		}

		//如果缓存中没有这个contentID的点赞状态，或者状态不是"1"，则需要从数据库查询
		if minCID > 0 && contentID < minCID {
			coldIDs = append(coldIDs, contentID)
			continue
		}
		result[contentID] = false
	}

	return result, coldIDs, nil
}
