package httpapi

import "net/http"

func Routes(handler *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handler.Health)
	mux.HandleFunc("/v1/limit/check", handler.Check)
	return mux
}
