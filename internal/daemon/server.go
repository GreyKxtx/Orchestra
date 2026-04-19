package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/store"
)

type ServerConfig struct {
	ProjectRoot string

	Address string
	Port    int

	ExcludeDirs []string
	SkipBackups bool

	ScanInterval      time.Duration
	MaxCacheFileBytes int64

	CacheEnabled bool
	CachePath    string // absolute or relative to project root
}

type Server struct {
	cfg ServerConfig

	projectRootAbs string
	projectID      string
	configHash     string

	state *State

	cachePath string
	url       string
	token     string

	httpServer *http.Server
	scanStop   chan struct{}

	// Metrics tracking
	mu              sync.Mutex
	lastScanMS      int64
	lastCacheLoadMS int64
	lastCacheSaveMS int64
	lastScanResult  *ScanResult
}

func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.ProjectRoot == "" {
		return nil, fmt.Errorf("project root is required")
	}

	rootAbs, err := filepath.Abs(cfg.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute project root: %w", err)
	}

	if cfg.Address == "" {
		cfg.Address = DefaultAddress
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = DefaultScanInterval
	}
	if cfg.MaxCacheFileBytes <= 0 {
		cfg.MaxCacheFileBytes = DefaultMaxCacheFileBytes
	}

	projectID, err := store.ComputeProjectID(rootAbs)
	if err != nil {
		return nil, err
	}
	configHash, err := store.ComputeConfigHash(cfg.ExcludeDirs, cfg.SkipBackups, cfg.MaxCacheFileBytes, ProtocolVersion)
	if err != nil {
		return nil, err
	}

	cachePath := cfg.CachePath
	if cachePath == "" {
		cachePath = filepath.Join(rootAbs, ".orchestra", "cache.json")
	} else if !filepath.IsAbs(cachePath) {
		cachePath = filepath.Join(rootAbs, cachePath)
	}

	seed := map[string]store.FileRef{}
	var cacheLoadMS int64
	if cfg.CacheEnabled {
		start := time.Now()
		if cached, ok, err := store.LoadCacheIfExists(cachePath); err == nil && ok {
			if cached.ProjectID == projectID && cached.ConfigHash == configHash {
				seed = cached.Files
			}
		}
		cacheLoadMS = time.Since(start).Milliseconds()
	}

	st, err := NewState(rootAbs, projectID, configHash, cfg.ExcludeDirs, cfg.SkipBackups, cfg.MaxCacheFileBytes, seed)
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:            cfg,
		projectRootAbs: rootAbs,
		projectID:      projectID,
		configHash:     configHash,
		state:          st,
		cachePath:      cachePath,
		token:          generateToken(),
		scanStop:       make(chan struct{}),
	}
	// Store initial cache load time
	s.mu.Lock()
	s.lastCacheLoadMS = cacheLoadMS
	s.mu.Unlock()

	mux := http.NewServeMux()
	s.registerHandlers(mux)
	s.httpServer = &http.Server{
		Handler: mux,
	}

	return s, nil
}

func (s *Server) URL() string   { return s.url }
func (s *Server) Token() string { return s.token }

func (s *Server) Run(ctx context.Context) error {
	// Initial scan to warm state.
	start := time.Now()
	scanRes, err := s.state.ScanOnce(ctx)
	scanMS := time.Since(start).Milliseconds()
	if err != nil {
		return fmt.Errorf("initial scan failed: %w", err)
	}
	var cacheSaveMS int64
	if s.cfg.CacheEnabled {
		start = time.Now()
		_ = store.SaveCache(s.cachePath, s.state.ToCache())
		cacheSaveMS = time.Since(start).Milliseconds()
	}

	// Get cache load time from NewServer (passed via closure or stored)
	// For now, we'll track it separately - cacheLoadMS is measured during NewServer
	s.mu.Lock()
	s.lastScanMS = scanMS
	s.lastCacheSaveMS = cacheSaveMS
	s.lastScanResult = &scanRes
	s.mu.Unlock()

	addr := net.JoinHostPort(s.cfg.Address, fmt.Sprintf("%d", s.cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	// Resolve actual address (useful if port was 0).
	s.url = "http://" + ln.Addr().String()

	// Write discovery file (best-effort; daemon can still run without it).
	_ = WriteDiscovery(s.projectRootAbs, DiscoveryInfo{
		ProtocolVersion: ProtocolVersion,
		ProjectRoot:     s.projectRootAbs,
		ProjectID:       s.projectID,
		URL:             s.url,
		Token:           s.token,
		PID:             os.Getpid(),
	})

	// Periodic scan loop.
	go s.scanLoop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		close(s.scanStop)
		_ = RemoveDiscovery(s.projectRootAbs)
		return nil
	case err := <-errCh:
		close(s.scanStop)
		_ = RemoveDiscovery(s.projectRootAbs)
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) scanLoop() {
	t := time.NewTicker(s.cfg.ScanInterval)
	defer t.Stop()

	for {
		select {
		case <-s.scanStop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ScanInterval)
			start := time.Now()
			res, err := s.state.ScanOnce(ctx)
			scanMS := time.Since(start).Milliseconds()
			cancel()
			var cacheSaveMS int64
			if err == nil && s.cfg.CacheEnabled && res.Changed > 0 {
				start = time.Now()
				_ = store.SaveCache(s.cachePath, s.state.ToCache())
				cacheSaveMS = time.Since(start).Milliseconds()
			}
			if err == nil {
				s.mu.Lock()
				s.lastScanMS = scanMS
				s.lastCacheSaveMS = cacheSaveMS
				s.lastScanResult = &res
				s.mu.Unlock()
			}
		}
	}
}
