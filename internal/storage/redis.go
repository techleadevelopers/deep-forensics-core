package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis wrapper.
type Redis struct{ Client *redis.Client }

// NewRedis conecta ao Redis via URL.
func NewRedis(ctx context.Context, url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Redis{Client: c}, nil
}

// Close encerra a conexão.
func (r *Redis) Close() { _ = r.Client.Close() }

// RateLimit implementa sliding window per-tenant/minute.
// Retorna (allowed, remaining, resetIn).
func (r *Redis) RateLimit(ctx context.Context, tenantID string, perMin int) (bool, int, time.Duration, error) {
	key := "rl:" + tenantID + ":" + time.Now().UTC().Format("200601021504")
	count, err := r.Client.Incr(ctx, key).Result()
	if err != nil {
		return false, 0, 0, err
	}
	if count == 1 {
		r.Client.Expire(ctx, key, 60*time.Second)
	}
	allowed := int(count) <= perMin
	remaining := perMin - int(count)
	if remaining < 0 {
		remaining = 0
	}
	return allowed, remaining, 60 * time.Second, nil
}

// IdempotencyGet devolve o body salvo, se houver.
func (r *Redis) IdempotencyGet(ctx context.Context, tenantID, key string) (string, error) {
	return r.Client.Get(ctx, "idem:"+tenantID+":"+key).Result()
}

// IdempotencySet grava o body por 24h.
func (r *Redis) IdempotencySet(ctx context.Context, tenantID, key, body string) error {
	return r.Client.Set(ctx, "idem:"+tenantID+":"+key, body, 24*time.Hour).Err()
}

// GetJSON carrega um valor JSON tipado. Retorna (false, nil) em cache miss.
func (r *Redis) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	raw, err := r.Client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, err
	}
	return true, nil
}

// SetJSON grava um valor JSON com TTL.
func (r *Redis) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.Client.Set(ctx, key, raw, ttl).Err()
}

func ResultCacheKey(tenantID, sha256, profile string) string {
	return "verify:result:" + tenantID + ":" + profile + ":" + sha256
}

func StageCacheKey(stage, sha256, version, configHash string) string {
	return "verify:stage:" + stage + ":" + version + ":" + configHash + ":" + sha256
}
