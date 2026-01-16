package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type payload struct {
	Key          string  `json:"key,omitempty"`
	UserID       string  `json:"user_id,omitempty"`
	DeviceID     string  `json:"device_id,omitempty"`
	JWT          string  `json:"jwt,omitempty"`
	Algorithm    string  `json:"algorithm"`
	Limit        int64   `json:"limit,omitempty"`
	WindowMs     int64   `json:"window_ms,omitempty"`
	Capacity     int64   `json:"capacity,omitempty"`
	RefillPerSec float64 `json:"refill_per_sec,omitempty"`
	LeakPerSec   float64 `json:"leak_per_sec,omitempty"`
	Cost         int64   `json:"cost,omitempty"`
}

func main() {
	var (
		url         = flag.String("url", "http://127.0.0.1:8080/v1/limit/check", "target URL")
		concurrency = flag.Int("concurrency", 8, "number of workers")
		duration    = flag.Duration("duration", 10*time.Second, "test duration")
		qps         = flag.Int("qps", 200, "total QPS (approx)")
	)

	req := payload{
		Algorithm:    "token_bucket",
		Key:          "bench:key",
		Capacity:     100,
		RefillPerSec: 50,
		Cost:         1,
	}

	flag.StringVar(&req.Key, "key", req.Key, "rate limit key")
	flag.StringVar(&req.UserID, "user_id", "", "user id key")
	flag.StringVar(&req.DeviceID, "device_id", "", "device id key")
	flag.StringVar(&req.JWT, "jwt", "", "jwt token key")
	flag.StringVar(&req.Algorithm, "algorithm", req.Algorithm, "algorithm")
	flag.Int64Var(&req.Limit, "limit", 0, "limit for window algorithms")
	flag.Int64Var(&req.WindowMs, "window_ms", 0, "window size in ms")
	flag.Int64Var(&req.Capacity, "capacity", req.Capacity, "capacity for bucket algorithms")
	flag.Float64Var(&req.RefillPerSec, "refill_per_sec", req.RefillPerSec, "refill per sec (token bucket)")
	flag.Float64Var(&req.LeakPerSec, "leak_per_sec", 0, "leak per sec (leaky bucket)")
	flag.Int64Var(&req.Cost, "cost", req.Cost, "cost per request")

	flag.Parse()

	body, err := json.Marshal(req)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	var (
		total     int64
		okCount   int64
		errorCount int64
		durationsMu sync.Mutex
		durations    []time.Duration
	)

	ticker := time.NewTicker(time.Second / time.Duration(max(1, *qps)))
	defer ticker.Stop()

	var wg sync.WaitGroup
	wg.Add(*concurrency)

	for i := 0; i < *concurrency; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					start := time.Now()
					resp, err := client.Post(*url, "application/json", bytes.NewReader(body))
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
						continue
					}
					_ = resp.Body.Close()
					atomic.AddInt64(&total, 1)
					if resp.StatusCode < 500 {
						atomic.AddInt64(&okCount, 1)
					}
					elapsed := time.Since(start)
					durationsMu.Lock()
					durations = append(durations, elapsed)
					durationsMu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	durationsMu.Lock()
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	durationsMu.Unlock()

	printStats(total, okCount, errorCount, durations)
}

func printStats(total, okCount, errorCount int64, samples []time.Duration) {
	if len(samples) == 0 {
		fmt.Println("no samples collected")
		return
	}
	p50 := percentile(samples, 0.50)
	p95 := percentile(samples, 0.95)
	p99 := percentile(samples, 0.99)
	min := samples[0]
	max := samples[len(samples)-1]
	avg := average(samples)

	fmt.Printf("total=%d ok=%d errors=%d\n", total, okCount, errorCount)
	fmt.Printf("min=%s p50=%s p95=%s p99=%s max=%s avg=%s\n",
		min, p50, p95, p99, max, avg,
	)
}

func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(samples))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(samples) {
		idx = len(samples) - 1
	}
	return samples[idx]
}

func average(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	var total time.Duration
	for _, s := range samples {
		total += s
	}
	return total / time.Duration(len(samples))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
