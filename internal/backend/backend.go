package backend

import "context"

type Result struct {
	Allowed       bool  `json:"allowed"`
	Remaining     int64 `json:"remaining"`
	ResetAtMs     int64 `json:"reset_at_ms"`
	RetryAfterMs  int64 `json:"retry_after_ms"`
	CurrentCount  int64 `json:"current_count,omitempty"`
	ComputedCount int64 `json:"computed_count,omitempty"`
}

type Backend interface {
	TokenBucketAllow(ctx context.Context, key string, capacity int64, refillPerSec float64, cost int64) (Result, error)
	LeakyBucketAllow(ctx context.Context, key string, capacity int64, leakPerSec float64, cost int64) (Result, error)
	FixedWindowAllow(ctx context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error)
	SlidingWindowLogAllow(ctx context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error)
	SlidingWindowCounterAllow(ctx context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error)
	Close() error
}
