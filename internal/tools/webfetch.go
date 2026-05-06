package tools

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// WebFetchRequest is the input for the webfetch tool.
type WebFetchRequest struct {
	URL      string `json:"url"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// WebFetchResponse is the output of the webfetch tool.
type WebFetchResponse struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

// WebFetch fetches a URL and returns its text content.
// Private/loopback/link-local IPs are blocked (SSRF protection).
func (r *Runner) WebFetch(ctx context.Context, req WebFetchRequest) (*WebFetchResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}

	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is empty")
	}
	// Scheme validation — only http/https allowed.
	lower := strings.ToLower(rawURL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return nil, fmt.Errorf("unsupported scheme: only http and https are allowed")
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = r.webMaxContentBytes
	}
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}

	timeout := r.webFetchTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: ssrfSafeDialer(),
		},
	}

	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	httpReq.Header.Set("User-Agent", "Orchestra/1.0 (AI assistant fetch)")
	httpReq.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.8")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read body up to maxBytes+1 to detect truncation.
	limited := io.LimitReader(resp.Body, int64(maxBytes)+1)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	truncated := len(bodyBytes) > maxBytes
	if truncated {
		bodyBytes = bodyBytes[:maxBytes]
		// Trim to valid UTF-8 boundary.
		for !utf8.Valid(bodyBytes) && len(bodyBytes) > 0 {
			bodyBytes = bodyBytes[:len(bodyBytes)-1]
		}
	}

	contentType := resp.Header.Get("Content-Type")
	var title, content string
	if strings.Contains(contentType, "html") || isLikelyHTML(bodyBytes) {
		title, content = extractTextFromHTML(string(bodyBytes))
	} else {
		content = string(bodyBytes)
	}

	return &WebFetchResponse{
		URL:       resp.Request.URL.String(),
		Title:     title,
		Content:   content,
		Truncated: truncated,
	}, nil
}

// ssrfSafeDialer returns a DialContext function that resolves DNS and rejects
// private/loopback/link-local addresses before making the connection.
func ssrfSafeDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	base := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}

		// Reject raw IP literals that are private/loopback before DNS lookup.
		if ip := net.ParseIP(host); ip != nil {
			if isBlockedIP(ip) {
				return nil, fmt.Errorf("blocked: private/loopback/link-local address %s", ip)
			}
			return base.DialContext(ctx, network, addr)
		}

		// Resolve hostname and check each returned IP.
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("DNS lookup failed for %q: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses resolved for %q", host)
		}
		for _, ia := range ips {
			if isBlockedIP(ia.IP) {
				return nil, fmt.Errorf("blocked: %q resolves to private/loopback/link-local IP %s", host, ia.IP)
			}
		}

		// Dial using the first resolved address directly (prevents re-resolution).
		target := net.JoinHostPort(ips[0].IP.String(), port)
		return base.DialContext(ctx, network, target)
	}
}

// isBlockedIP reports whether ip is private, loopback, link-local, or otherwise
// unsuitable for outbound requests from an AI agent.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// extractTextFromHTML parses HTML and returns (title, body-text).
func extractTextFromHTML(src string) (title, text string) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", src
	}

	var titleBuf, textBuf strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			switch tag {
			case "script", "style", "noscript", "iframe", "svg", "canvas":
				return
			case "title":
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						titleBuf.WriteString(c.Data)
					}
				}
				return
			}
		}
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				textBuf.WriteString(t)
				textBuf.WriteByte('\n')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return strings.TrimSpace(titleBuf.String()), strings.TrimSpace(textBuf.String())
}

// isLikelyHTML returns true if the first 512 bytes contain an HTML tag.
func isLikelyHTML(b []byte) bool {
	sample := strings.ToLower(string(b[:min512(len(b))]))
	return strings.Contains(sample, "<html") ||
		strings.Contains(sample, "<!doctype") ||
		strings.Contains(sample, "<head") ||
		strings.Contains(sample, "<body")
}

func min512(n int) int {
	if n > 512 {
		return 512
	}
	return n
}
