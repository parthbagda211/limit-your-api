package backend

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisBackend struct {
	client *redis.Client
}

func NewRedisBackend(addr, password string, db int) (*RedisBackend, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}
	return &RedisBackend{client: client}, nil
}

func (r *RedisBackend) TokenBucketAllow(ctx context.Context, key string, capacity int64, refillPerSec float64, cost int64) (Result, error) {
	if capacity <= 0 || refillPerSec <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()
	ttlMs := int64(math.Ceil((float64(capacity)/refillPerSec)*1000.0)) + 1000
	res, err := tokenBucketScript.Run(ctx, r.client, []string{"tb:" + key}, capacity, refillPerSec, cost, nowMs, ttlMs).Result()
	if err != nil {
		return Result{}, err
	}
	return parseResult(res), nil
}

func (r *RedisBackend) LeakyBucketAllow(ctx context.Context, key string, capacity int64, leakPerSec float64, cost int64) (Result, error) {
	if capacity <= 0 || leakPerSec <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()
	ttlMs := int64(math.Ceil((float64(capacity)/leakPerSec)*1000.0)) + 1000
	res, err := leakyBucketScript.Run(ctx, r.client, []string{"lb:" + key}, capacity, leakPerSec, cost, nowMs, ttlMs).Result()
	if err != nil {
		return Result{}, err
	}
	return parseResult(res), nil
}

func (r *RedisBackend) FixedWindowAllow(ctx context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error) {
	if limit <= 0 || windowMs <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()
	res, err := fixedWindowScript.Run(ctx, r.client, []string{key}, limit, windowMs, cost, nowMs).Result()
	if err != nil {
		return Result{}, err
	}
	return parseResult(res), nil
}

func (r *RedisBackend) SlidingWindowLogAllow(ctx context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error) {
	if limit <= 0 || windowMs <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()
	res, err := slidingLogScript.Run(ctx, r.client, []string{"swl:" + key, "swl:" + key + ":seq"}, limit, windowMs, cost, nowMs).Result()
	if err != nil {
		return Result{}, err
	}
	return parseResult(res), nil
}

func (r *RedisBackend) SlidingWindowCounterAllow(ctx context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error) {
	if limit <= 0 || windowMs <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()
	res, err := slidingCounterScript.Run(ctx, r.client, []string{"swc:" + key}, limit, windowMs, cost, nowMs).Result()
	if err != nil {
		return Result{}, err
	}
	return parseResult(res), nil
}

func (r *RedisBackend) Close() error {
	return r.client.Close()
}

func parseResult(value interface{}) Result {
	items, ok := value.([]interface{})
	if !ok || len(items) < 4 {
		return Result{}
	}
	return Result{
		Allowed:       toInt64(items[0]) == 1,
		Remaining:     toInt64(items[1]),
		ResetAtMs:     toInt64(items[2]),
		RetryAfterMs:  toInt64(items[3]),
		CurrentCount:  getOptionalInt(items, 4),
		ComputedCount: getOptionalInt(items, 5),
	}
}

func getOptionalInt(items []interface{}, idx int) int64 {
	if idx >= len(items) {
		return 0
	}
	return toInt64(items[idx])
}

func toInt64(value interface{}) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now_ms = tonumber(ARGV[4])
local ttl_ms = tonumber(ARGV[5])

local tokens = tonumber(redis.call("HGET", key, "tokens"))
local last_ms = tonumber(redis.call("HGET", key, "last_ms"))

if tokens == nil then tokens = capacity end
if last_ms == nil then last_ms = now_ms end

if now_ms < last_ms then last_ms = now_ms end

local refill_tokens = (now_ms - last_ms) / 1000 * refill
tokens = math.min(capacity, tokens + refill_tokens)
last_ms = now_ms

local allowed = 0
if tokens >= cost then
	allowed = 1
	tokens = tokens - cost
end

redis.call("HSET", key, "tokens", tokens, "last_ms", last_ms)
redis.call("PEXPIRE", key, ttl_ms)

local remaining = math.floor(tokens)
local reset_at = now_ms + math.ceil(((capacity - tokens) / refill) * 1000)
local retry_after = 0
if allowed == 0 then
	local missing = cost - tokens
	retry_after = math.ceil((missing / refill) * 1000)
end

return {allowed, remaining, reset_at, retry_after}
`)

var leakyBucketScript = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local leak = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now_ms = tonumber(ARGV[4])
local ttl_ms = tonumber(ARGV[5])

local water = tonumber(redis.call("HGET", key, "water"))
local last_ms = tonumber(redis.call("HGET", key, "last_ms"))

if water == nil then water = 0 end
if last_ms == nil then last_ms = now_ms end

if now_ms < last_ms then last_ms = now_ms end

local leaked = (now_ms - last_ms) / 1000 * leak
water = math.max(0, water - leaked)
last_ms = now_ms

local allowed = 0
if water + cost <= capacity then
	allowed = 1
	water = water + cost
end

redis.call("HSET", key, "water", water, "last_ms", last_ms)
redis.call("PEXPIRE", key, ttl_ms)

local remaining = math.floor(capacity - water)
local reset_at = now_ms + math.ceil((water / leak) * 1000)
local retry_after = 0
if allowed == 0 then
	local overflow = (water + cost) - capacity
	retry_after = math.ceil((overflow / leak) * 1000)
end

return {allowed, remaining, reset_at, retry_after}
`)

var fixedWindowScript = redis.NewScript(`
local base_key = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now_ms = tonumber(ARGV[4])

local window_start = now_ms - (now_ms % window_ms)
local key = base_key .. ":" .. window_start
local count = redis.call("INCRBY", key, cost)
redis.call("PEXPIRE", key, window_ms + 1000)

local allowed = 1
if count > limit then allowed = 0 end

local reset_at = window_start + window_ms
local retry_after = 0
if allowed == 0 then retry_after = reset_at - now_ms end

return {allowed, limit - count, reset_at, retry_after, count}
`)

var slidingLogScript = redis.NewScript(`
local key = KEYS[1]
local seq_key = KEYS[2]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now_ms = tonumber(ARGV[4])

local cutoff = now_ms - window_ms
redis.call("ZREMRANGEBYSCORE", key, 0, cutoff)
local count = redis.call("ZCARD", key)

local allowed = 0
if count + cost <= limit then
	allowed = 1
	for i = 1, cost do
		local seq = redis.call("INCR", seq_key)
		redis.call("ZADD", key, now_ms, now_ms .. ":" .. seq)
	end
	count = count + cost
end

redis.call("PEXPIRE", key, window_ms + 1000)
redis.call("PEXPIRE", seq_key, window_ms + 1000)

local reset_at = now_ms + window_ms
if count > 0 then
	local oldest = redis.call("ZRANGE", key, 0, 0, "WITHSCORES")
	if oldest[2] ~= nil then
		reset_at = tonumber(oldest[2]) + window_ms
	end
end

local retry_after = 0
if allowed == 0 then retry_after = reset_at - now_ms end

return {allowed, limit - count, reset_at, retry_after, count}
`)

var slidingCounterScript = redis.NewScript(`
local base_key = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now_ms = tonumber(ARGV[4])

local current_start = now_ms - (now_ms % window_ms)
local prev_start = current_start - window_ms

local current_key = base_key .. ":" .. current_start
local prev_key = base_key .. ":" .. prev_start

local current_count = tonumber(redis.call("GET", current_key) or "0")
local prev_count = tonumber(redis.call("GET", prev_key) or "0")

local elapsed = now_ms - current_start
local weight = (window_ms - elapsed) / window_ms
local computed = (prev_count * weight) + current_count

local allowed = 0
if computed + cost <= limit then
	allowed = 1
	current_count = redis.call("INCRBY", current_key, cost)
	computed = computed + cost
end

redis.call("PEXPIRE", current_key, window_ms + 1000)
redis.call("PEXPIRE", prev_key, window_ms + 1000)

local reset_at = current_start + window_ms
local retry_after = 0
if allowed == 0 then retry_after = reset_at - now_ms end

return {allowed, math.floor(limit - computed), reset_at, retry_after, current_count, math.ceil(computed)}
`)

var _ = fmt.Sprintf
