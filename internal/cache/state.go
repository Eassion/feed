package cache

import (
	"context"
	_ "embed"
	"strconv"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/toggle_hash_state.lua
var toggleHashStateLua string

var toggleHashStateScript = redis.NewScript(toggleHashStateLua)

type HashStateStore struct {
	client *redis.Client
}

func NewHashStateStore(client *redis.Client) *HashStateStore {
	return &HashStateStore{client: client}
}

func (s *HashStateStore) SetState(ctx context.Context, key string, field int64, active bool) (bool, error) {
	if s == nil || s.client == nil {
		return true, nil
	}

	action := "unset"
	if active {
		action = "set"
	}

	result, err := toggleHashStateScript.Run(
		ctx,
		s.client,
		[]string{key},
		strconv.FormatInt(field, 10),
		action,
	).Int()
	if err != nil {
		return false, err
	}

	return result == 1, nil
}

// 批量获取key对应的多个field的状态，返回一个map，key是field，value是bool表示状态；
// 如果某个field在缓存中不存在，则认为状态为false
func (s *HashStateStore) BatchGetStates(ctx context.Context, key string, ids []int64) (map[int64]bool, error) {
	result := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	if s == nil || s.client == nil {
		return result, nil
	}

	fields := make([]string, 0, len(ids))
	for _, id := range ids {
		fields = append(fields, strconv.FormatInt(id, 10))
	}

	values, err := s.client.HMGet(ctx, key, fields...).Result()
	if err != nil {
		return nil, err
	}

	for idx, value := range values {
		if value == nil {
			result[ids[idx]] = false
			continue
		}
		if str, ok := value.(string); ok {
			result[ids[idx]] = str == "1"
			continue
		}
		result[ids[idx]] = false
	}

	return result, nil
}
