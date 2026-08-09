package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeKind selects which connectivity check to run.
type ProbeKind int

const (
	// ProbeModels hits OpenAI-compatible GET {api_base}/models (auth + reachability).
	ProbeModels ProbeKind = iota
	// ProbeChat sends a minimal chat completion without tools.
	ProbeChat
)

// ProbeResult is the outcome of Probe.
type ProbeResult struct {
	OK            bool
	Kind          ProbeKind
	URL           string
	Model         string
	HTTPCode      int
	Duration      time.Duration
	Err           string // empty when OK
	Hint          string // optional user-facing guidance (RU/EN mix ok)
	ContextTokens int    // from /v1/models max_model_len when discovered
	MaxTokensCap  int    // safe completion cap for ContextTokens
}

// Probe checks that api_base (+ api_key) actually work — not merely that fields are filled.
func Probe(ctx context.Context, cfg LLMConfig, kind ProbeKind) ProbeResult {
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if base == "" {
		return ProbeResult{Kind: kind, Err: "api_base пустой", Hint: "Укажи URL сервера (например http://localhost:8000/v1)"}
	}
	// Normalize: clients expect host root or …/v1; chat client appends /v1 if missing.
	out := ProbeResult{Kind: kind, URL: base, Model: cfg.Model}
	start := time.Now()
	defer func() { out.Duration = time.Since(start) }()

	// Always try to learn the server context window (cheap GET /models).
	if lim, err := DiscoverModelLimits(ctx, cfg); err == nil && lim.ContextTokens > 0 {
		out.ContextTokens = lim.ContextTokens
		out.MaxTokensCap = lim.MaxTokensCap
	}

	switch kind {
	case ProbeModels:
		code, err := probeModelsHTTP(ctx, base, cfg.APIKey)
		out.HTTPCode = code
		if err != nil {
			out.Err = err.Error()
			out.Hint = hintFromHTTP(code, out.Err, cfg)
			return out
		}
		out.OK = true
		return out
	default: // ProbeChat
		if strings.TrimSpace(cfg.Model) == "" {
			out.Err = "model не выбран"
			out.Hint = "Сначала выбери модель"
			return out
		}
		cfgCopy := cfg
		if cfgCopy.TimeoutS <= 0 {
			cfgCopy.TimeoutS = 30
		}
		if out.ContextTokens > 0 {
			ApplyDiscoveredLimits(&cfgCopy, ModelLimits{
				ContextTokens: out.ContextTokens,
				MaxTokensCap:  out.MaxTokensCap,
			})
		}
		client := NewClient(cfgCopy)
		pingCtx, cancel := context.WithTimeout(ctx, time.Duration(cfgCopy.TimeoutS)*time.Second)
		defer cancel()
		_, err := client.Complete(pingCtx, CompleteRequest{
			Messages: []Message{{Role: RoleUser, Content: "ping"}},
		})
		if err != nil {
			out.Err = err.Error()
			out.HTTPCode = extractStatusCode(err.Error())
			out.Hint = hintFromHTTP(out.HTTPCode, out.Err, cfg)
			return out
		}
		out.OK = true
		out.HTTPCode = 200
		return out
	}
}

func probeModelsHTTP(ctx context.Context, apiBase, apiKey string) (int, error) {
	base := strings.TrimRight(apiBase, "/")
	candidates := []string{base + "/models"}
	if !strings.HasSuffix(strings.ToLower(base), "/v1") {
		candidates = append([]string{base + "/v1/models"}, candidates...)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	var lastStatus int
	var lastErr error
	for _, url := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Accept", "application/json")
		if k := strings.TrimSpace(apiKey); k != "" {
			req.Header.Set("Authorization", "Bearer "+k)
		}
		setNgrokBypass(req, apiBase)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		lastStatus = resp.StatusCode
		if resp.StatusCode == http.StatusOK {
			return resp.StatusCode, nil
		}
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		// 401/403 — no point trying alternate path with same key.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return resp.StatusCode, lastErr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("models endpoint unreachable")
	}
	return lastStatus, lastErr
}

func extractStatusCode(msg string) int {
	var code int
	if _, err := fmt.Sscanf(msg, "request failed (status %d)", &code); err == nil {
		return code
	}
	if _, err := fmt.Sscanf(msg, "API error (status %d)", &code); err == nil {
		return code
	}
	if _, err := fmt.Sscanf(msg, "API returned status %d", &code); err == nil {
		return code
	}
	return 0
}

func hintFromHTTP(code int, errMsg string, cfg LLMConfig) string {
	low := strings.ToLower(errMsg)
	switch {
	case code == 401 || code == 403 || strings.Contains(low, "unauthorized"):
		return "API key отклонён сервером — проверь ключ / Authorization"
	case code == 404 || strings.Contains(low, "offline") || strings.Contains(low, "ngrok"):
		return "Endpoint недоступен — проверь URL / туннель ngrok"
	case strings.Contains(low, "enable-auto-tool-choice") || strings.Contains(low, "tool-call-parser"):
		return "vLLM: нужен --enable-auto-tool-choice --tool-call-parser hermes (или llm.tool_choice: omit)"
	case code >= 500:
		return "Сервер LLM вернул 5xx — смотри логи vLLM/LM Studio"
	case strings.Contains(low, "connection refused") || strings.Contains(low, "timeout") || strings.Contains(low, "no such host"):
		return "Нет сети до api_base — сервер не запущен или URL неверный"
	default:
		if strings.EqualFold(cfg.Provider, "vllm") || cfg.Provider == "" {
			return "Проверь, что vLLM слушает api_base и модель загружена"
		}
		return ""
	}
}

// Summary is a short one-line status for toasts / notices.
func (r ProbeResult) Summary() string {
	if r.OK {
		ms := r.Duration.Milliseconds()
		ctx := ""
		if r.ContextTokens > 0 {
			ctx = fmt.Sprintf(" · ctx %d", r.ContextTokens)
			if r.MaxTokensCap > 0 {
				ctx += fmt.Sprintf(" · max_out≤%d", r.MaxTokensCap)
			}
		}
		switch r.Kind {
		case ProbeModels:
			return fmt.Sprintf("LLM OK · models%s · %dms", ctx, ms)
		default:
			return fmt.Sprintf("LLM OK · chat · %s%s · %dms", r.Model, ctx, ms)
		}
	}
	msg := r.Err
	if r.Hint != "" {
		msg = r.Err + " — " + r.Hint
	}
	if len([]rune(msg)) > 160 {
		msg = string([]rune(msg)[:157]) + "…"
	}
	return "LLM ✗ · " + msg
}
