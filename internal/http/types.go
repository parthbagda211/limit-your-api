package httpapi

type CheckRequest struct {
	Key          string  `json:"key"`
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

type CheckResponse struct {
	Key           string `json:"key"`
	Algorithm     string `json:"algorithm"`
	Allowed       bool   `json:"allowed"`
	Remaining     int64  `json:"remaining"`
	ResetAtMs     int64  `json:"reset_at_ms"`
	RetryAfterMs  int64  `json:"retry_after_ms"`
	CurrentCount  int64  `json:"current_count,omitempty"`
	ComputedCount int64  `json:"computed_count,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
