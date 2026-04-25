package cache

import (
	"context"
	"encoding/json"
	"strconv"

	"feed/internal/model"
	"github.com/redis/go-redis/v9"
)

type CommentStore struct {
	client *redis.Client
}

func NewCommentStore(client *redis.Client) *CommentStore {
	return &CommentStore{client: client}
}

func (s *CommentStore) GetCommentObjects(ctx context.Context, commentIDs []int64) (map[int64]model.CommentItem, []int64, error) {
	result := make(map[int64]model.CommentItem, len(commentIDs))
	if len(commentIDs) == 0 {
		return result, nil, nil
	}
	if s == nil || s.client == nil {
		return result, append([]int64(nil), commentIDs...), nil
	}

	keys := make([]string, 0, len(commentIDs))
	for _, commentID := range commentIDs {
		keys = append(keys, CommentObjectKey(commentID))
	}

	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, nil, err
	}

	missing := make([]int64, 0)
	for idx, value := range values {
		if value == nil {
			missing = append(missing, commentIDs[idx])
			continue
		}

		raw, ok := value.(string)
		if !ok || raw == "" {
			missing = append(missing, commentIDs[idx])
			continue
		}

		var item model.CommentItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			missing = append(missing, commentIDs[idx])
			continue
		}
		result[item.ID] = item
	}

	return result, missing, nil
}

func (s *CommentStore) SetCommentObjects(ctx context.Context, items []model.CommentItem) error {
	if s == nil || s.client == nil || len(items) == 0 {
		return nil
	}

	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, item := range items {
			payload, err := json.Marshal(item)
			if err != nil {
				return err
			}
			pipe.Set(ctx, CommentObjectKey(item.ID), payload, 0)
		}
		return nil
	})
	return err
}

func (s *CommentStore) DeleteCommentObject(ctx context.Context, commentID int64) error {
	if s == nil || s.client == nil || commentID <= 0 {
		return nil
	}
	return s.client.Del(ctx, CommentObjectKey(commentID)).Err()
}

//批量删除评论对象缓存
func (s *CommentStore) DeleteCommentObjects(ctx context.Context, commentIDs []int64) error {
	if s == nil || s.client == nil || len(commentIDs) == 0 {
		return nil
	}

	keys := make([]string, 0, len(commentIDs))
	for _, commentID := range commentIDs {
		if commentID > 0 {
			keys = append(keys, CommentObjectKey(commentID))
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return s.client.Del(ctx, keys...).Err()
}

func (s *CommentStore) ContentIndexExists(ctx context.Context, contentID int64) (bool, error) {
	return s.indexExists(ctx, CommentContentIndexKey(contentID), CommentContentIndexInitKey(contentID))
}

func (s *CommentStore) ReplyIndexExists(ctx context.Context, rootID int64) (bool, error) {
	return s.indexExists(ctx, CommentRootRepliesKey(rootID), CommentRootRepliesInitKey(rootID))
}

func (s *CommentStore) TryAcquireContentIndexLock(ctx context.Context, contentID int64) (bool, error) {
	return s.tryAcquireLock(ctx, CommentContentIndexLockKey(contentID))
}

func (s *CommentStore) TryAcquireReplyIndexLock(ctx context.Context, rootID int64) (bool, error) {
	return s.tryAcquireLock(ctx, CommentRootRepliesLockKey(rootID))
}

func (s *CommentStore) AddToContentIndex(ctx context.Context, contentID, commentID int64) error {
	if s == nil || s.client == nil || contentID <= 0 || commentID <= 0 {
		return nil
	}

	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.ZAdd(ctx, CommentContentIndexKey(contentID), redis.Z{Score: float64(commentID), Member: strconv.FormatInt(commentID, 10)})
		pipe.Set(ctx, CommentContentIndexInitKey(contentID), "1", 0)
		return nil
	})
	return err
}

func (s *CommentStore) RebuildContentIndex(ctx context.Context, contentID int64, commentIDs []int64) error {
	if s == nil || s.client == nil || contentID <= 0 {
		return nil
	}

	key := CommentContentIndexKey(contentID)
	initKey := CommentContentIndexInitKey(contentID)
	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, key)
		if len(commentIDs) > 0 {
			entries := make([]redis.Z, 0, len(commentIDs))
			for _, commentID := range commentIDs {
				entries = append(entries, redis.Z{Score: float64(commentID), Member: strconv.FormatInt(commentID, 10)})
			}
			pipe.ZAdd(ctx, key, entries...)
		}
		pipe.Set(ctx, initKey, "1", 0)
		return nil
	})
	return err
}

func (s *CommentStore) RebuildReplyIndex(ctx context.Context, rootID int64, commentIDs []int64) error {
	if s == nil || s.client == nil || rootID <= 0 {
		return nil
	}

	key := CommentRootRepliesKey(rootID)
	initKey := CommentRootRepliesInitKey(rootID)
	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, key)
		if len(commentIDs) > 0 {
			entries := make([]redis.Z, 0, len(commentIDs))
			for _, commentID := range commentIDs {
				entries = append(entries, redis.Z{Score: float64(commentID), Member: strconv.FormatInt(commentID, 10)})
			}
			pipe.ZAdd(ctx, key, entries...)
		}
		pipe.Set(ctx, initKey, "1", 0)
		return nil
	})
	return err
}

func (s *CommentStore) ListContentIndex(ctx context.Context, contentID int64, cursor string, limit int) ([]int64, string, bool, error) {
	return s.listIndex(ctx, CommentContentIndexKey(contentID), cursor, limit)
}

func (s *CommentStore) ListReplyIndex(ctx context.Context, rootID int64, cursor string, limit int) ([]int64, string, bool, error) {
	return s.listIndex(ctx, CommentRootRepliesKey(rootID), cursor, limit)
}

// 失效评论索引缓存，通常在内容被删除或者评论被批量删除时调用
func (s *CommentStore) InvalidateContentIndex(ctx context.Context, contentID int64) error {
	return s.invalidateIndex(ctx, CommentContentIndexKey(contentID), CommentContentIndexInitKey(contentID), CommentContentIndexLockKey(contentID))
}

func (s *CommentStore) InvalidateReplyIndex(ctx context.Context, rootID int64) error {
	return s.invalidateIndex(ctx, CommentRootRepliesKey(rootID), CommentRootRepliesInitKey(rootID), CommentRootRepliesLockKey(rootID))
}

func (s *CommentStore) indexExists(ctx context.Context, key, initKey string) (bool, error) {
	if s == nil || s.client == nil {
		return false, nil
	}

	count, err := s.client.Exists(ctx, initKey, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *CommentStore) tryAcquireLock(ctx context.Context, key string) (bool, error) {
	if s == nil || s.client == nil {
		return true, nil
	}
	return s.client.SetNX(ctx, key, "1", CommentIndexRebuildLockTTL).Result()
}

func (s *CommentStore) listIndex(ctx context.Context, key, cursor string, limit int) ([]int64, string, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	if s == nil || s.client == nil {
		return []int64{}, "", false, nil
	}

	max := "+inf"
	if cursor != "" {
		max = "(" + cursor
	}

	members, err := s.client.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
		Max:    max,
		Min:    "-inf",
		Offset: 0,
		Count:  int64(limit + 1),
	}).Result()
	if err != nil {
		return nil, "", false, err
	}

	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}

	ids := make([]int64, 0, len(members))
	for _, member := range members {
		commentID, err := strconv.ParseInt(member, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, commentID)
	}

	nextCursor := ""
	if hasMore && len(ids) > 0 {
		nextCursor = strconv.FormatInt(ids[len(ids)-1], 10)
	}

	return ids, nextCursor, hasMore, nil
}

func (s *CommentStore) invalidateIndex(ctx context.Context, key, initKey, lockKey string) error {
	if s == nil || s.client == nil {
		return nil
	}
	//同时删除3个key
	return s.client.Del(ctx, key, initKey, lockKey).Err()
}
