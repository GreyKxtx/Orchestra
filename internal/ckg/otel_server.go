package ckg

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
)

const otlpMaxBodyBytes = 10 * 1024 * 1024 // 10 MB

// OTLPServer is a minimal OTLP/HTTP receiver that accepts traces and ingests
// them into the CKG store. Only JSON encoding is supported (not protobuf).
// Bind to 127.0.0.1 by default — never expose to the network.
type OTLPServer struct {
	store    *Store
	rootDir  string
	httpSrv  *http.Server
	listener net.Listener
}

// NewOTLPServer creates a new OTLP/HTTP server but does not start listening.
func NewOTLPServer(store *Store, rootDir string) *OTLPServer {
	s := &OTLPServer{store: store, rootDir: rootDir}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", s.handleTraces)
	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// ListenAndServe starts the HTTP server on addr (e.g. "127.0.0.1:4318").
// Blocks until the server is stopped or returns an error.
func (s *OTLPServer) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("otlp server: listen %s: %w", addr, err)
	}
	s.listener = ln
	log.Printf("[otlp] listening on http://%s/v1/traces (JSON only; set OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/json)", ln.Addr())
	return s.httpSrv.Serve(ln)
}

// Addr returns the network address the server is bound to, or "" if not started.
func (s *OTLPServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Shutdown gracefully stops the server.
func (s *OTLPServer) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func (s *OTLPServer) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("Content-Type")
	// Strip parameters like "; charset=utf-8"
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}
	if ct == "application/x-protobuf" {
		http.Error(w, `unsupported encoding: protobuf. Set OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/json`, http.StatusUnsupportedMediaType)
		return
	}

	body := http.MaxBytesReader(w, r.Body, otlpMaxBodyBytes)
	var reader io.Reader = body

	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			http.Error(w, "invalid gzip body", http.StatusBadRequest)
			return
		}
		defer gz.Close()
		reader = gz
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	traces, err := ParseOTLPJSON(data, s.rootDir)
	if err != nil {
		http.Error(w, "parse OTLP JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var ingested, failed int
	for _, td := range traces {
		if ierr := s.store.IngestTrace(ctx, td); ierr != nil {
			log.Printf("[otlp] ingest trace %s: %v", td.TraceID, ierr)
			failed++
		} else {
			ingested++
		}
	}
	if ingested > 0 || failed == 0 {
		log.Printf("[otlp] ingested %d trace(s) (%d span(s) total)", ingested, countSpans(traces[:ingested]))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"partialSuccess": map[string]any{}})
}

func countSpans(traces []TraceData) int {
	n := 0
	for _, td := range traces {
		n += len(td.Spans)
	}
	return n
}
