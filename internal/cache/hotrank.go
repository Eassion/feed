package cache

import (
	"context"
	_ "embed"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type HotRankStore struct {
	client *redis.Client
}

//go:embed lua/merge_hotrank_delta.lua
var mergeHotRankDeltaLua string

var mergeHotRankDeltaScript = redis.NewScript(mergeHotRankDeltaLua)

func NewHotRankStore(client *redis.Client) *HotRankStore {
	return &HotRankStore{client: client}
}

func (s *HotRankStore) AddDelta(ctx context.Context, contentID int64, delta float64) error {
	if s == nil || s.client == nil || contentID <= 0 || delta == 0 {
		return nil
	}

	return s.client.HIncrByFloat(ctx, FeedHotGlobalIncKey(hotRankShard(contentID)), strconv.FormatInt(contentID, 10), delta).Err()
}

func (s *HotRankStore) RemoveContent(ctx context.Context, contentID int64) error {
	if s == nil || s.client == nil || contentID <= 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	pipe.ZRem(ctx, FeedHotGlobalKey, strconv.FormatInt(contentID, 10))
	pipe.HDel(ctx, FeedHotGlobalIncKey(hotRankShard(contentID)), strconv.FormatInt(contentID, 10))
	_, err := pipe.Exec(ctx)
	return err
}

func (s *HotRankStore) MergeDelta(ctx context.Context) (int64, error) {
	if s == nil || s.client == nil {
		return 0, nil
	}

	var total int64
	for shard := int64(0); shard < FeedHotShardCount; shard++ {
		result, err := mergeHotRankDeltaScript.Run(ctx, s.client, []string{FeedHotGlobalIncKey(shard), FeedHotGlobalKey}).Int64()
		if err != nil {
			return 0, err
		}
		total += result
	}
	return total, nil
}

func (s *HotRankStore) BuildSnapshot(ctx context.Context, snapshotSize int64, snapshotTTL time.Duration) (string, map[int64]float64, error) {
	if s == nil || s.client == nil {
		return "", map[int64]float64{}, nil
	}
	if snapshotSize <= 0 {
		snapshotSize = 1000
	}

	items, err := s.client.ZRevRangeWithScores(ctx, FeedHotGlobalKey, 0, snapshotSize-1).Result()
	if err != nil && err != redis.Nil {
		return "", nil, err
	}

	snapshotID := strconv.FormatInt(time.Now().UnixMilli(), 10)
	snapshotKey := FeedHotSnapshotKey(snapshotID)
	scoreMap := make(map[int64]float64, len(items))

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, snapshotKey)
	if len(items) > 0 {
		members := make([]redis.Z, 0, len(items))
		for _, item := range items {
			contentID, ok := parseHotRankMemberID(item.Member)
			if !ok {
				continue
			}
			scoreMap[contentID] = item.Score
			members = append(members, redis.Z{Member: strconv.FormatInt(contentID, 10), Score: item.Score})
		}
		if len(members) > 0 {
			pipe.ZAdd(ctx, snapshotKey, members...)
		}
	}
	pipe.Set(ctx, FeedHotLatestSnapshotKey, snapshotID, snapshotTTL)
	if snapshotTTL > 0 {
		pipe.Expire(ctx, snapshotKey, snapshotTTL)
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return "", nil, err
	}

	return snapshotID, scoreMap, nil
}

func (s *HotRankStore) ReplaceGlobal(ctx context.Context, scores map[int64]float64) error {
	if s == nil || s.client == nil {
		return nil
	}

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, FeedHotGlobalKey)
	if len(scores) > 0 {
		members := make([]redis.Z, 0, len(scores))
		for contentID, score := range scores {
			if contentID <= 0 {
				continue
			}
			members = append(members, redis.Z{
				Member: strconv.FormatInt(contentID, 10),
				Score:  score,
			})
		}
		if len(members) > 0 {
			pipe.ZAdd(ctx, FeedHotGlobalKey, members...)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *HotRankStore) TryAcquireFastUpdateLock(ctx context.Context, bucket string, ttl time.Duration) (bool, error) {
	if s == nil || s.client == nil || bucket == "" {
		return false, nil
	}
	if ttl <= 0 {
		ttl = time.Minute
	}

	return s.client.SetNX(ctx, FeedHotGlobalFastLockKey(bucket), "1", ttl).Result()
}

func (s *HotRankStore) ReleaseFastUpdateLock(ctx context.Context, bucket string) error {
	if s == nil || s.client == nil || bucket == "" {
		return nil
	}

	return s.client.Del(ctx, FeedHotGlobalFastLockKey(bucket)).Err()
}

func (s *HotRankStore) TryAcquireColdUpdateLock(ctx context.Context, date string, ttl time.Duration) (bool, error) {
	if s == nil || s.client == nil || date == "" {
		return false, nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}

	return s.client.SetNX(ctx, FeedHotGlobalColdLockKey(date), "1", ttl).Result()
}

func (s *HotRankStore) ReleaseColdUpdateLock(ctx context.Context, date string) error {
	if s == nil || s.client == nil || date == "" {
		return nil
	}

	return s.client.Del(ctx, FeedHotGlobalColdLockKey(date)).Err()
}

func parseHotRankMemberID(member any) (int64, bool) {
	switch value := member.(type) {
	case string:
		id, err := strconv.ParseInt(value, 10, 64)
		return id, err == nil
	case []byte:
		id, err := strconv.ParseInt(string(value), 10, 64)
		return id, err == nil
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}

func hotRankShard(contentID int64) int64 {
	if contentID <= 0 || FeedHotShardCount <= 0 {
		return 0
	}
	return contentID % FeedHotShardCount
}
