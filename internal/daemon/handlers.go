package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) registerHandlers(mux *http.ServeMux) {
	// Token-protected endpoints (per vNext safety contract).
	mux.HandleFunc("/api/v1/health", s.withAuth(s.handleHealth))
	mux.HandleFunc("/api/v1/files", s.withAuth(s.handleFiles))
	mux.HandleFunc("/api/v1/file", s.withAuth(s.handleFileGet))
	mux.HandleFunc("/api/v1/file/read", s.withAuth(s.handleFileRead))
	mux.HandleFunc("/api/v1/search", s.withAuth(s.handleSearch))
	mux.HandleFunc("/api/v1/context", s.withAuth(s.handleContext))
	mux.HandleFunc("/api/v1/refresh", s.withAuth(s.handleRefresh))
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If token is empty (shouldn't happen in vNext), allow for backward compatibility.
		if strings.TrimSpace(s.token) == "" {
			next(w, r)
			return
		}
		if !validAuth(r, s.token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
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
	if strings.TrimSpace(r.Header.Get("X-Orchestra-Token")) == token {
		return true
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:          "ok",
		DaemonVersion:   DaemonVersion,
		ProtocolVersion: ProtocolVersion,
		ProjectRoot:     s.projectRootAbs,
		ProjectID:       s.projectID,
	})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	files := s.state.SnapshotFiles()

	// Optional pagination.
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if offset > 0 {
		if offset >= len(files) {
			files = nil
		} else {
			files = files[offset:]
		}
	}
	if limit > 0 && limit < len(files) {
		files = files[:limit]
	}

	writeJSON(w, http.StatusOK, ListFilesResponse{Files: files})
}

func (s *Server) handleFileGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		writeError(w, http.StatusBadRequest, "missing path")
		return
	}

	content, meta, err := s.readFileWithMeta(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ReadFileResponse{
		Path:    meta.Path,
		Size:    meta.Size,
		MTime:   meta.MTime,
		Content: string(content),
	})
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ReadFileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "missing path")
		return
	}

	content, meta, err := s.readFileWithMeta(r.Context(), req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ReadFileResponse{
		Path:    meta.Path,
		Size:    meta.Size,
		MTime:   meta.MTime,
		Content: string(content),
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req SearchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "query cannot be empty")
		return
	}
	if req.Options.MaxMatchesPerFile <= 0 {
		req.Options.MaxMatchesPerFile = 10
	}
	if req.Options.ContextLines < 0 {
		req.Options.ContextLines = 0
	}

	matches := s.search(ctxOrBackground(r.Context()), req.Query, req.Options)
	writeJSON(w, http.StatusOK, SearchResponse{Matches: matches})
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ContextRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := s.Context(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := s.Refresh(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) readFileWithMeta(ctx context.Context, path string) ([]byte, FileMeta, error) {
	meta, ok := s.state.GetFileMeta(path)
	if !ok {
		// Still allow reading (if file appeared but scan hasn't run yet).
		key, err := normalizeRelPath(path)
		if err != nil {
			return nil, FileMeta{}, err
		}
		meta = FileMeta{Path: key}
	}

	b, m, err := s.state.ReadFile(meta.Path)
	if err != nil {
		return nil, FileMeta{}, err
	}
	if m.Size > 0 {
		meta.Size = m.Size
		meta.MTime = m.MTime
	}
	// Keep hash from state if available.
	if meta.Hash == "" {
		meta.Hash = m.Hash
	}
	return b, meta, nil
}

func (s *Server) search(ctx context.Context, query string, opts SearchOptions) []SearchMatch {
	states := s.snapshotStates()
	queryLower := strings.ToLower(query)

	var out []SearchMatch
	for _, f := range states {
		// Respect max matches per file.
		if opts.MaxMatchesPerFile <= 0 {
			opts.MaxMatchesPerFile = 10
		}

		content := f.content
		if len(content) == 0 {
			b, _, err := s.state.ReadFile(f.Path)
			if err != nil {
				continue
			}
			content = b
		}
		if isProbablyBinary(content) {
			continue
		}

		fileMatches := searchInText(f.Path, string(content), query, queryLower, opts)
		out = append(out, fileMatches...)
	}
	return out
}

type scoredFile struct {
	path  string
	score int
}

func (s *Server) buildContext(ctx context.Context, query string, limits ContextLimits, excludeDirs []string) []ContextFile {
	states := s.snapshotStates()
	tokens := tokenize(query)
	exclude := buildExcludeSet(excludeDirs)

	scored := make([]scoredFile, 0, len(states))
	for _, f := range states {
		if isExcluded(f.Path, exclude) {
			continue
		}
		score := scoreFile(f, tokens)
		if score > 0 {
			scored = append(scored, scoredFile{path: f.Path, score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].path < scored[j].path
		}
		return scored[i].score > scored[j].score
	})

	focusSet := make(map[string]struct{}, len(scored))
	focusOrder := make([]string, 0, len(scored))
	for _, sf := range scored {
		if len(focusOrder) >= limits.MaxFiles {
			break
		}
		if _, ok := focusSet[sf.path]; ok {
			continue
		}
		focusSet[sf.path] = struct{}{}
		focusOrder = append(focusOrder, sf.path)
	}

	result := make([]ContextFile, 0, limits.MaxFiles)
	budget := limits.MaxTotalBytes

	addFile := func(path string, content []byte) {
		if budget <= 0 || len(result) >= limits.MaxFiles {
			return
		}
		orig := len(content)
		truncated := false
		if limits.MaxBytesPerFile > 0 && int64(len(content)) > limits.MaxBytesPerFile {
			content = content[:limits.MaxBytesPerFile]
			truncated = true
		}
		if int64(len(content)) > budget {
			content = content[:budget]
			truncated = true
		}
		if len(content) == 0 {
			return
		}
		budget -= int64(len(content))
		cf := ContextFile{Path: path, Content: string(content)}
		if truncated {
			cf.Truncated = true
			cf.OriginalSize = orig
		}
		result = append(result, cf)
	}

	// 1) focus files first (may read from disk)
	for _, p := range focusOrder {
		b, _, err := s.state.ReadFile(p)
		if err != nil {
			continue
		}
		addFile(p, b)
		if budget <= 0 || len(result) >= limits.MaxFiles {
			return result
		}
	}

	// 2) fill with other cached small files only (no disk reads)
	paths := make([]string, 0, len(states))
	cacheByPath := make(map[string][]byte, len(states))
	for _, f := range states {
		if isExcluded(f.Path, exclude) {
			continue
		}
		if _, ok := focusSet[f.Path]; ok {
			continue
		}
		if len(f.content) == 0 {
			continue
		}
		paths = append(paths, f.Path)
		cacheByPath[f.Path] = f.content
	}
	sort.Strings(paths)
	for _, p := range paths {
		addFile(p, cacheByPath[p])
		if budget <= 0 || len(result) >= limits.MaxFiles {
			return result
		}
	}

	return result
}

func isExcluded(p string, exclude map[string]struct{}) bool {
	if len(exclude) == 0 {
		return false
	}
	// p uses forward slashes.
	for dir := range exclude {
		if dir == "" {
			continue
		}
		dir = strings.Trim(dir, "/")
		if dir == "" {
			continue
		}
		if p == dir || strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

func buildExcludeSet(excludeDirs []string) map[string]struct{} {
	if len(excludeDirs) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(excludeDirs))
	for _, d := range excludeDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		m[strings.Trim(d, "/")] = struct{}{}
	}
	return m
}

func (s *Server) snapshotStates() []*FileState {
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()

	out := make([]*FileState, 0, len(s.state.files))
	for _, f := range s.state.files {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func tokenize(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	parts := strings.Fields(q)
	out := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		p = strings.Trim(p, "\"'`.,:;()[]{}")
		if len(p) >= 3 {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = append(out, q)
	}
	return out
}

func scoreFile(f *FileState, tokens []string) int {
	p := strings.ToLower(f.Path)
	score := 0
	for _, t := range tokens {
		if strings.Contains(p, t) {
			score++
		}
	}
	if len(f.content) == 0 {
		return score
	}
	c := strings.ToLower(string(f.content))
	for _, t := range tokens {
		if strings.Contains(c, t) {
			score += 2
		}
	}
	return score
}

func searchInText(path string, content string, query string, queryLower string, opts SearchOptions) []SearchMatch {
	var matches []SearchMatch
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		if len(matches) >= opts.MaxMatchesPerFile {
			break
		}

		lineToSearch := line
		if opts.CaseInsensitive {
			lineToSearch = strings.ToLower(line)
		}

		searchQuery := query
		if opts.CaseInsensitive {
			searchQuery = queryLower
		}

		if strings.Contains(lineToSearch, searchQuery) {
			before := collectContext(lines, i, opts.ContextLines, true)
			after := collectContext(lines, i, opts.ContextLines, false)
			matches = append(matches, SearchMatch{
				Path:          path,
				Line:          i + 1,
				LineText:      strings.TrimRight(line, "\r\n"),
				ContextBefore: before,
				ContextAfter:  after,
			})
		}
	}

	return matches
}

func collectContext(lines []string, currentLine int, contextLines int, before bool) []string {
	if contextLines <= 0 {
		return nil
	}
	var context []string
	start := currentLine - contextLines
	end := currentLine

	if before {
		if start < 0 {
			start = 0
		}
		for i := start; i < end; i++ {
			context = append(context, strings.TrimRight(lines[i], "\r\n"))
		}
		return context
	}

	start = currentLine + 1
	end = currentLine + 1 + contextLines
	if end > len(lines) {
		end = len(lines)
	}
	for i := start; i < end; i++ {
		context = append(context, strings.TrimRight(lines[i], "\r\n"))
	}
	return context
}

func isProbablyBinary(data []byte) bool {
	max := len(data)
	if max > 8000 {
		max = 8000
	}
	for i := 0; i < max; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("empty body")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 2<<20)) // 2MB
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if err == io.EOF {
			return fmt.Errorf("empty body")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
