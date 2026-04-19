package cli

import (
	"fmt"
	"io"
)

// Metrics tracks performance metrics for CLI operations
type Metrics struct {
	// Discovery and connection
	DiscoveryMS int64 // Time to discover daemon URL
	HealthMS    int64 // Time for health check

	// Daemon operations
	RefreshMS int64 // Time for refresh call
	ContextMS int64 // Time for context call

	// Context results
	ContextFiles          int // Number of files in context
	ContextBytesTotal     int // Total bytes in context
	ContextTruncatedFiles int // Number of truncated files

	// Daemon internal metrics (from API response)
	DaemonScanMS      int64 // Daemon scan time
	DaemonCacheLoadMS int64 // Daemon cache load time
	DaemonCacheFastOK int   // Files validated via mtime+size
	DaemonCacheHashed int   // Files that needed hash computation
}

func (m *Metrics) Print(w io.Writer) {
	fmt.Fprintf(w, "[orchestra] Performance metrics:\n")
	if m.DiscoveryMS > 0 {
		fmt.Fprintf(w, "  discovery_ms=%d\n", m.DiscoveryMS)
	}
	if m.HealthMS > 0 {
		fmt.Fprintf(w, "  health_ms=%d\n", m.HealthMS)
	}
	if m.RefreshMS > 0 {
		fmt.Fprintf(w, "  refresh_ms=%d\n", m.RefreshMS)
	}
	if m.ContextMS > 0 {
		fmt.Fprintf(w, "  context_ms=%d\n", m.ContextMS)
	}
	if m.ContextFiles > 0 {
		fmt.Fprintf(w, "  context_files=%d\n", m.ContextFiles)
		fmt.Fprintf(w, "  context_bytes_total=%d\n", m.ContextBytesTotal)
		if m.ContextTruncatedFiles > 0 {
			fmt.Fprintf(w, "  context_truncated_files=%d\n", m.ContextTruncatedFiles)
		}
	}
	if m.DaemonScanMS > 0 {
		fmt.Fprintf(w, "  daemon_scan_ms=%d\n", m.DaemonScanMS)
	}
	if m.DaemonCacheLoadMS > 0 {
		fmt.Fprintf(w, "  daemon_cache_load_ms=%d\n", m.DaemonCacheLoadMS)
	}
	if m.DaemonCacheFastOK > 0 {
		fmt.Fprintf(w, "  daemon_cache_fast_ok=%d\n", m.DaemonCacheFastOK)
	}
	if m.DaemonCacheHashed > 0 {
		fmt.Fprintf(w, "  daemon_cache_hashed=%d\n", m.DaemonCacheHashed)
	}
}
