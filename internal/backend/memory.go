package backend

import (
	"context"
	"math"
	"sync"
	"time"
)

type MemoryBackend struct {
	mu              sync.Mutex
	tokenBuckets    map[string]*tokenBucketState
	leakyBuckets    map[string]*leakyBucketState
	fixedWindows    map[string]*fixedWindowState
	slidingLogs     map[string][]int64
	slidingCounters map[string]*slidingCounterState
}

type tokenBucketState struct {
	tokens float64
	lastMs int64
}

type leakyBucketState struct {
	water float64
	lastMs int64
}

type fixedWindowState struct {
	count        int64
	windowStartMs int64
}

type slidingCounterState struct {
	windowStartMs int64
	currentCount  int64
	prevCount     int64
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		tokenBuckets:    make(map[string]*tokenBucketState),
		leakyBuckets:    make(map[string]*leakyBucketState),
		fixedWindows:    make(map[string]*fixedWindowState),
		slidingLogs:     make(map[string][]int64),
		slidingCounters: make(map[string]*slidingCounterState),
	}
}

func (m *MemoryBackend) TokenBucketAllow(_ context.Context, key string, capacity int64, refillPerSec float64, cost int64) (Result, error) {
	if capacity <= 0 || refillPerSec <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()

	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.tokenBuckets[key]
	if state == nil {
		state = &tokenBucketState{
			tokens: float64(capacity),
			lastMs: nowMs,
		}
		m.tokenBuckets[key] = state
	}

	elapsedMs := nowMs - state.lastMs
	refill := (float64(elapsedMs) / 1000.0) * refillPerSec
	state.tokens = math.Min(float64(capacity), state.tokens+refill)
	state.lastMs = nowMs

	allowed := state.tokens >= float64(cost)
	if allowed {
		state.tokens -= float64(cost)
	}

	remaining := int64(math.Floor(state.tokens))
	resetAtMs := nowMs + int64(math.Ceil(((float64(capacity)-state.tokens)/refillPerSec)*1000.0))
	retryAfterMs := int64(0)
	if !allowed {
		missing := float64(cost) - state.tokens
		retryAfterMs = int64(math.Ceil((missing/refillPerSec)*1000.0))
	}

	return Result{
		Allowed:      allowed,
		Remaining:    remaining,
		ResetAtMs:    resetAtMs,
		RetryAfterMs: retryAfterMs,
	}, nil
}

func (m *MemoryBackend) LeakyBucketAllow(_ context.Context, key string, capacity int64, leakPerSec float64, cost int64) (Result, error) {
	if capacity <= 0 || leakPerSec <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()

	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.leakyBuckets[key]
	if state == nil {
		state = &leakyBucketState{
			water: 0,
			lastMs: nowMs,
		}
		m.leakyBuckets[key] = state
	}

	elapsedMs := nowMs - state.lastMs
	leak := (float64(elapsedMs) / 1000.0) * leakPerSec
	state.water = math.Max(0, state.water-leak)
	state.lastMs = nowMs

	allowed := state.water+float64(cost) <= float64(capacity)
	if allowed {
		state.water += float64(cost)
	}

	remaining := int64(math.Floor(float64(capacity) - state.water))
	resetAtMs := nowMs + int64(math.Ceil((state.water/leakPerSec)*1000.0))
	retryAfterMs := int64(0)
	if !allowed {
		overflow := state.water + float64(cost) - float64(capacity)
		retryAfterMs = int64(math.Ceil((overflow/leakPerSec)*1000.0))
	}

	return Result{
		Allowed:      allowed,
		Remaining:    remaining,
		ResetAtMs:    resetAtMs,
		RetryAfterMs: retryAfterMs,
	}, nil
}

func (m *MemoryBackend) FixedWindowAllow(_ context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error) {
	if limit <= 0 || windowMs <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()

	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.fixedWindows[key]
	if state == nil || nowMs-state.windowStartMs >= windowMs {
		state = &fixedWindowState{
			count:         0,
			windowStartMs: nowMs - (nowMs % windowMs),
		}
		m.fixedWindows[key] = state
	}

	allowed := state.count+cost <= limit
	if allowed {
		state.count += cost
	}

	resetAtMs := state.windowStartMs + windowMs
	retryAfterMs := int64(0)
	if !allowed {
		retryAfterMs = resetAtMs - nowMs
	}

	return Result{
		Allowed:      allowed,
		Remaining:    limit - state.count,
		ResetAtMs:    resetAtMs,
		RetryAfterMs: retryAfterMs,
		CurrentCount: state.count,
	}, nil
}

func (m *MemoryBackend) SlidingWindowLogAllow(_ context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error) {
	if limit <= 0 || windowMs <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()

	m.mu.Lock()
	defer m.mu.Unlock()

	logs := m.slidingLogs[key]
	cutoff := nowMs - windowMs
	kept := logs[:0]
	for _, ts := range logs {
		if ts > cutoff {
			kept = append(kept, ts)
		}
	}
	logs = kept

	allowed := int64(len(logs))+cost <= limit
	if allowed {
		for i := int64(0); i < cost; i++ {
			logs = append(logs, nowMs)
		}
	}
	m.slidingLogs[key] = logs

	var resetAtMs int64
	if len(logs) > 0 {
		resetAtMs = logs[0] + windowMs
	} else {
		resetAtMs = nowMs + windowMs
	}

	retryAfterMs := int64(0)
	if !allowed {
		retryAfterMs = resetAtMs - nowMs
	}

	return Result{
		Allowed:      allowed,
		Remaining:    limit - int64(len(logs)),
		ResetAtMs:    resetAtMs,
		RetryAfterMs: retryAfterMs,
		CurrentCount: int64(len(logs)),
	}, nil
}

func (m *MemoryBackend) SlidingWindowCounterAllow(_ context.Context, key string, limit int64, windowMs int64, cost int64) (Result, error) {
	if limit <= 0 || windowMs <= 0 || cost <= 0 {
		return Result{}, nil
	}
	nowMs := time.Now().UnixMilli()
	currentWindowStart := nowMs - (nowMs % windowMs)

	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.slidingCounters[key]
	if state == nil {
		state = &slidingCounterState{
			windowStartMs: currentWindowStart,
			currentCount:  0,
			prevCount:     0,
		}
		m.slidingCounters[key] = state
	}

	if state.windowStartMs != currentWindowStart {
		state.prevCount = state.currentCount
		state.currentCount = 0
		state.windowStartMs = currentWindowStart
	}

	elapsed := nowMs - state.windowStartMs
	weight := float64(windowMs-elapsed) / float64(windowMs)
	computed := float64(state.prevCount)*weight + float64(state.currentCount)
	allowed := computed+float64(cost) <= float64(limit)
	if allowed {
		state.currentCount += cost
		computed += float64(cost)
	}

	resetAtMs := state.windowStartMs + windowMs
	retryAfterMs := int64(0)
	if !allowed {
		retryAfterMs = resetAtMs - nowMs
	}

	return Result{
		Allowed:       allowed,
		Remaining:     int64(math.Max(0, float64(limit)-computed)),
		ResetAtMs:     resetAtMs,
		RetryAfterMs:  retryAfterMs,
		CurrentCount:  state.currentCount,
		ComputedCount: int64(math.Ceil(computed)),
	}, nil
}

func (m *MemoryBackend) Close() error {
	return nil
}
