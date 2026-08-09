package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type HTTPOptions struct {
	Addr  string // e.g. "127.0.0.1:0"
	Token string

	// Health is returned by GET /health.
	Health any
}

// StartHTTP starts an optional local HTTP server for debugging.
//
// Endpoints:
// - GET  /health (token required)
// - POST /rpc    (token required) - JSON-RPC 2.0 request/response
func StartHTTP(ctx context.Context, h Handler, opts HTTPOptions) (baseURL string, stop func() error, err error) {
	if h == nil {
		return "", nil, fmt.Errorf("handler is nil")
	}
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		return "", nil, fmt.Errorf("http debug server must bind to 127.0.0.1")
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return "", nil, fmt.Errorf("token is required")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", auth(token, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, opts.Health)
	}))
	mux.HandleFunc("/rpc", auth(token, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		req, perr := parsePayload(body)
		if perr != nil {
			writeJSON(w, http.StatusBadRequest, Response{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error: &Error{
					Code:    perr.Code,
					Message: perr.Message,
					Data:    perr.Data,
				},
			})
			return
		}

		// Notifications: no JSON-RPC response (HTTP 204).
		if req.IsNotification {
			_, _ = h.Handle(ctxOr(r.Context(), ctx), req.Method, req.Params)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		res, callErr := h.Handle(ctxOr(r.Context(), ctx), req.Method, req.Params)
		if callErr != nil {
			writeJSON(w, http.StatusOK, Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   toRPCError(callErr),
			})
			return
		}
		writeJSON(w, http.StatusOK, Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  res,
		})
	}))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	stop = func() error {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		_ = ln.Close()
		return err
	}

	// Stop server on ctx cancellation.
	go func() {
		<-ctx.Done()
		_ = stop()
	}()

	baseURL = "http://" + ln.Addr().String()
	return baseURL, stop, nil
}

func auth(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validAuth(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func validAuth(r *http.Request, token string) bool {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		got := strings.TrimSpace(h[len("bearer "):])
		return got == token
	}
	// Allow an explicit header for convenience in local dev.
	if strings.TrimSpace(r.Header.Get("X-Orchestra-Token")) == token {
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ctxOr(a, b context.Context) context.Context {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	// Prefer request context.
	return a
}
