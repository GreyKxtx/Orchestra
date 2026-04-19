package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/store"
)

// Refresh rescans the project "now" and updates server metrics/cache.
// This is the same logic used by POST /api/v1/refresh.
func (s *Server) Refresh(ctx context.Context) (RefreshResponse, error) {
	start := time.Now()
	res, err := s.state.ScanOnce(ctx)
	scanMS := time.Since(start).Milliseconds()
	if err != nil {
		return RefreshResponse{}, err
	}

	var cacheSaveMS int64
	if s.cfg.CacheEnabled && res.Changed > 0 {
		start = time.Now()
		_ = store.SaveCache(s.cachePath, s.state.ToCache())
		cacheSaveMS = time.Since(start).Milliseconds()
	}

	// Cache load time is measured during NewServer.
	s.mu.Lock()
	cacheLoadMS := s.lastCacheLoadMS
	s.lastScanMS = scanMS
	s.lastCacheLoadMS = cacheLoadMS
	s.lastCacheSaveMS = cacheSaveMS
	s.lastScanResult = &res
	s.mu.Unlock()

	metrics := &Metrics{
		ScanMS:        scanMS,
		CacheLoadMS:   cacheLoadMS,
		CacheSaveMS:   cacheSaveMS,
		CacheFastOK:   res.CacheFastOK,
		CacheHashed:   res.CacheHashed,
		ScanFilesSeen: res.FilesSeen,
	}

	return RefreshResponse{
		Status:       "ok",
		ScannedFiles: res.Scanned,
		ChangedFiles: res.Changed,
		Metrics:      metrics,
	}, nil
}

// Context builds a context response in-process, without HTTP/JSON overhead.
// This is the same logic used by POST /api/v1/context.
func (s *Server) Context(ctx context.Context, req ContextRequest) (ContextResponse, error) {
	if strings.TrimSpace(req.Query) == "" {
		return ContextResponse{}, fmt.Errorf("query cannot be empty")
	}
	if req.LimitKB <= 0 {
		req.LimitKB = 50
	}

	limits := ContextLimits{
		MaxFiles:        30,
		MaxTotalBytes:   int64(req.LimitKB) * 1024,
		MaxBytesPerFile: DefaultMaxBytesPerFile,
	}
	if req.Limits != nil {
		if req.Limits.MaxFiles > 0 {
			limits.MaxFiles = req.Limits.MaxFiles
		}
		if req.Limits.MaxTotalBytes > 0 && req.Limits.MaxTotalBytes < limits.MaxTotalBytes {
			limits.MaxTotalBytes = req.Limits.MaxTotalBytes
		}
		if req.Limits.MaxBytesPerFile > 0 {
			limits.MaxBytesPerFile = req.Limits.MaxBytesPerFile
		}
	}

	files := s.buildContext(ctx, req.Query, limits, req.ExcludeDirs)

	var metrics *Metrics
	s.mu.Lock()
	if s.lastScanResult != nil {
		metrics = &Metrics{
			ScanMS:        s.lastScanMS,
			CacheLoadMS:   s.lastCacheLoadMS,
			CacheSaveMS:   s.lastCacheSaveMS,
			CacheFastOK:   s.lastScanResult.CacheFastOK,
			CacheHashed:   s.lastScanResult.CacheHashed,
			ScanFilesSeen: s.lastScanResult.FilesSeen,
		}
	}
	s.mu.Unlock()

	return ContextResponse{Files: files, Metrics: metrics}, nil
}
