package cache

import (
	"context"
	_ "embed"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/verify_and_renew_session.lua
var verifyAndRenewSessionLua string

var verifyAndRenewSessionScript = redis.NewScript(verifyAndRenewSessionLua)

type SessionStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewSessionStore(client *redis.Client, ttl time.Duration) *SessionStore {
	return &SessionStore{
		client: client,
		ttl:    ttl,
	}
}

func (s *SessionStore) Save(ctx context.Context, token string, userID int64) error {
	if s == nil || s.client == nil {
		return nil
	}

	tokenKey := UserSessionTokenKey(token)
	userKey := UserSessionUserKey(userID)

	oldToken, err := s.client.Get(ctx, userKey).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	//事务式更新，确保旧token和新token的一致性
	pipe := s.client.TxPipeline()
	if oldToken != "" && oldToken != token {
		pipe.Del(ctx, UserSessionTokenKey(oldToken))
	}
	pipe.Set(ctx, tokenKey, userID, s.ttl)
	pipe.Set(ctx, userKey, token, s.ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *SessionStore) Delete(ctx context.Context, token string, userID int64) error {
	if s == nil || s.client == nil {
		return nil
	}

	return s.client.Del(ctx, UserSessionTokenKey(token), UserSessionUserKey(userID)).Err()
}

func (s *SessionStore) VerifyAndRenew(ctx context.Context, token string, userID int64) (bool, error) {
	if s == nil || s.client == nil {
		return true, nil
	}

	result, err := verifyAndRenewSessionScript.Run(
		ctx,
		s.client,
		[]string{UserSessionTokenKey(token), UserSessionUserKey(userID)},
		strconv.FormatInt(userID, 10),
		token,
		strconv.FormatInt(int64(s.ttl/time.Second), 10),
	).Int()
	if err != nil {
		return false, err
	}

	return result == 1, nil
}
