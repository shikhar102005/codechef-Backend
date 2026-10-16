package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type CacheService interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, val interface{}, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	AllowRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	IsHealthy(ctx context.Context) bool
}

type CombinedCache struct {
	client     *redis.Client
	isRedis    bool
	inMemCache sync.Map
	rateLimMu  sync.Mutex
	rateLimits map[string][]time.Time
}

type inMemItem struct {
	data      []byte
	expiresAt time.Time
}

func NewCacheService(ctx context.Context, redisURL string) CacheService {
	c := &CombinedCache{
		rateLimits: make(map[string][]time.Time),
	}

	if redisURL == "" || redisURL == "none" || redisURL == "memory" {
		c.isRedis = false
		return c
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		opts = &redis.Options{Addr: redisURL, DialTimeout: 500 * time.Millisecond, MaxRetries: 1}
	} else {
		opts.DialTimeout = 500 * time.Millisecond
		opts.MaxRetries = 1
	}
	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err == nil {
		slog.Info("Connected to Redis server successfully", "addr", opts.Addr)
		c.client = client
		c.isRedis = true
	} else {
		slog.Warn("Redis unavailable or not reachable. Operating with resilient in-memory cache/rate-limiter fallback.")
		c.isRedis = false
	}

	return c
}

func (c *CombinedCache) IsHealthy(ctx context.Context) bool {
	if !c.isRedis || c.client == nil {
		return true // In-memory fallback is healthy
	}
	return c.client.Ping(ctx).Err() == nil
}

func (c *CombinedCache) Get(ctx context.Context, key string, dest interface{}) error {
	if c.isRedis && c.client != nil {
		val, err := c.client.Get(ctx, key).Bytes()
		if err == nil {
			return json.Unmarshal(val, dest)
		}
	}

	// In-memory fallback
	val, ok := c.inMemCache.Load(key)
	if !ok {
		return fmt.Errorf("cache miss")
	}
	item := val.(inMemItem)
	if time.Now().After(item.expiresAt) {
		c.inMemCache.Delete(key)
		return fmt.Errorf("cache expired")
	}
	return json.Unmarshal(item.data, dest)
}

func (c *CombinedCache) Set(ctx context.Context, key string, val interface{}, ttl time.Duration) error {
	bytes, err := json.Marshal(val)
	if err != nil {
		return err
	}

	if c.isRedis && c.client != nil {
		_ = c.client.Set(ctx, key, bytes, ttl).Err()
	}

	c.inMemCache.Store(key, inMemItem{
		data:      bytes,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (c *CombinedCache) Delete(ctx context.Context, keys ...string) error {
	if c.isRedis && c.client != nil {
		_ = c.client.Del(ctx, keys...).Err()
	}
	for _, k := range keys {
		c.inMemCache.Delete(k)
	}
	return nil
}

func (c *CombinedCache) AllowRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if c.isRedis && c.client != nil {
		pipe := c.client.TxPipeline()
		now := time.Now().UnixNano()
		clearBefore := now - window.Nanoseconds()

		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", clearBefore))
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
		countCmd := pipe.ZCard(ctx, key)
		pipe.Expire(ctx, key, window)

		_, err := pipe.Exec(ctx)
		if err == nil {
			return countCmd.Val() <= int64(limit), nil
		}
		// If redis failed, proceed to in-memory fallback
	}

	// In-memory token bucket rate limit
	c.rateLimMu.Lock()
	defer c.rateLimMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)
	valid := make([]time.Time, 0)
	for _, t := range c.rateLimits[key] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= limit {
		c.rateLimits[key] = valid
		return false, nil
	}
	valid = append(valid, now)
	c.rateLimits[key] = valid
	return true, nil
}
