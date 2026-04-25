package cache

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/zadd_trim.lua
var zaddTrimLua string

var zaddTrimScript = redis.NewScript(zaddTrimLua)

type InboxStore struct {
	client *redis.Client
	keepN  int64
}

func NewInboxStore(client *redis.Client, keepN int64) *InboxStore {
	return &InboxStore{
		client: client,
		keepN:  keepN,
	}
}

func (s *InboxStore) AddContent(ctx context.Context, userID, contentID int64, score float64) error {
	return s.AddBatch(ctx, userID, []InboxEntry{{ContentID: contentID, Score: score}})
}

type InboxEntry struct {
	ContentID int64
	Score     float64
}

func (s *InboxStore) AddBatch(ctx context.Context, userID int64, entries []InboxEntry) error {
	if s == nil || s.client == nil {
		return nil
	}

	if len(entries) == 0 {
		return s.markInitialized(ctx, userID)
	}

	args := make([]any, 0, 1+len(entries)*2)
	args = append(args, strconv.FormatInt(s.keepN, 10))
	for _, entry := range entries {
		args = append(args, fmt.Sprintf("%.6f", entry.Score), strconv.FormatInt(entry.ContentID, 10))
	}

	if err := s.markInitialized(ctx, userID); err != nil {
		return err
	}

	return zaddTrimScript.Run(ctx, s.client, []string{FollowInboxKey(userID)}, args...).Err()
}

func (s *InboxStore) Replace(ctx context.Context, userID int64, entries []InboxEntry) error {
	if s == nil || s.client == nil {
		return nil
	}

	key := FollowInboxKey(userID)
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, key)
	pipe.Set(ctx, FollowInboxInitKey(userID), "1", 0)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	return s.AddBatch(ctx, userID, entries)
}

func (s *InboxStore) List(ctx context.Context, userID int64, limit int64) ([]redis.Z, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}

	return s.client.ZRevRangeWithScores(ctx, FollowInboxKey(userID), 0, limit-1).Result()
}

func (s *InboxStore) Exists(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.client == nil {
		return false, nil
	}

	//判断两个key是否存在，存在一个就说明已经初始化过了
	exists, err := s.client.Exists(ctx, FollowInboxInitKey(userID), FollowInboxKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

func (s *InboxStore) TryAcquireRebuildLock(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.client == nil {
		return true, nil
	}

	return s.client.SetNX(ctx, FollowInboxRebuildLockKey(userID), "1", FollowInboxRebuildLockTTL).Result()
}

func (s *InboxStore) markInitialized(ctx context.Context, userID int64) error {
	if s == nil || s.client == nil {
		return nil
	}

	return s.client.Set(ctx, FollowInboxInitKey(userID), "1", 0).Err()
}
