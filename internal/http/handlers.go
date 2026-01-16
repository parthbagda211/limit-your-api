package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"rate-limiter-service/internal/backend"
)

type Handler struct {
	backend backend.Backend
}

func NewHandler(backend backend.Backend) *Handler {
	return &Handler{backend: backend}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid_json"})
		return
	}

	req.Algorithm = strings.ToLower(strings.TrimSpace(req.Algorithm))
	req.Key = strings.TrimSpace(req.Key)
	req.UserID = strings.TrimSpace(req.UserID)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.JWT = strings.TrimSpace(req.JWT)
	if req.JWT == "" {
		req.JWT = bearerToken(r.Header.Get("Authorization"))
	}
	if req.Key == "" {
		req.Key = buildKey(req)
	}
	if req.Key == "" || req.Algorithm == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "key_and_algorithm_required"})
		return
	}
	if req.Cost == 0 {
		req.Cost = 1
	}

	var (
		res backend.Result
		err error
	)

	switch req.Algorithm {
	case "token_bucket":
		if req.Capacity <= 0 || req.RefillPerSec <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "capacity_and_refill_per_sec_required"})
			return
		}
		res, err = h.backend.TokenBucketAllow(r.Context(), req.Key, req.Capacity, req.RefillPerSec, req.Cost)
	case "leaky_bucket":
		if req.Capacity <= 0 || req.LeakPerSec <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "capacity_and_leak_per_sec_required"})
			return
		}
		res, err = h.backend.LeakyBucketAllow(r.Context(), req.Key, req.Capacity, req.LeakPerSec, req.Cost)
	case "fixed_window":
		if req.Limit <= 0 || req.WindowMs <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "limit_and_window_ms_required"})
			return
		}
		res, err = h.backend.FixedWindowAllow(r.Context(), req.Key, req.Limit, req.WindowMs, req.Cost)
	case "sliding_window_log":
		if req.Limit <= 0 || req.WindowMs <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "limit_and_window_ms_required"})
			return
		}
		res, err = h.backend.SlidingWindowLogAllow(r.Context(), req.Key, req.Limit, req.WindowMs, req.Cost)
	case "sliding_window_counter":
		if req.Limit <= 0 || req.WindowMs <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "limit_and_window_ms_required"})
			return
		}
		res, err = h.backend.SlidingWindowCounterAllow(r.Context(), req.Key, req.Limit, req.WindowMs, req.Cost)
	default:
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "unsupported_algorithm"})
		return
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "backend_error"})
		return
	}

	w.Header().Set("X-RateLimit-Remaining", int64ToString(res.Remaining))
	w.Header().Set("X-RateLimit-Reset-Ms", int64ToString(res.ResetAtMs))
	w.Header().Set("X-RateLimit-Retry-After-Ms", int64ToString(res.RetryAfterMs))
	status := http.StatusOK
	if !res.Allowed {
		status = http.StatusTooManyRequests
	}

	writeJSON(w, status, CheckResponse{
		Key:           req.Key,
		Algorithm:     req.Algorithm,
		Allowed:       res.Allowed,
		Remaining:     res.Remaining,
		ResetAtMs:     res.ResetAtMs,
		RetryAfterMs:  res.RetryAfterMs,
		CurrentCount:  res.CurrentCount,
		ComputedCount: res.ComputedCount,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func int64ToString(v int64) string {
	return strconv.FormatInt(v, 10)
}

func buildKey(req CheckRequest) string {
	switch {
	case req.UserID != "":
		return "user:" + req.UserID
	case req.DeviceID != "":
		return "device:" + req.DeviceID
	case req.JWT != "":
		sum := sha256.Sum256([]byte(req.JWT))
		return "jwt:" + hex.EncodeToString(sum[:])
	default:
		return ""
	}
}

func bearerToken(header string) string {
	value := strings.TrimSpace(header)
	if value == "" {
		return ""
	}
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if strings.ToLower(parts[0]) != "bearer" {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
