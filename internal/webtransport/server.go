package webtransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/projects"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

// cookieName carries the bearer token for browser requests. The page is served
// with it as HttpOnly, so fetch() and WebSocket authenticate themselves and no
// credential is reachable from page scripts.
const cookieName = "orchestra"

type Options struct {
	// Addr to bind. Empty means "127.0.0.1:0". Loopback only.
	Addr string
	// Token is required; every endpoint checks it.
	Token string
	// Health is returned by GET /health.
	Health any
	// Assets, when non-nil, is served at /.
	Assets fs.FS
	// NewHandler builds a fresh handler for one connection and returns a
	// callback that attaches that connection's server to it. Called once per
	// accepted WebSocket.
	NewHandler func() (jsonrpc.Handler, func(*jsonrpc.Server))
	// Registry, when non-nil, enables /api/projects and the ?project= form of
	// /ws. Nil keeps the original single-core behaviour.
	Registry *projects.Registry
	// InitProject initialises a directory for POST /api/projects {"init":true}.
	// Injected rather than imported so this package does not depend on
	// internal/cli.
	InitProject func(ctx context.Context, root string) error
	// NewProjectHandler builds a handler for one project's core. Required when
	// Registry is set; NewHandler still serves the project-less /ws.
	NewProjectHandler func(c *core.Core) (jsonrpc.Handler, func(*jsonrpc.Server))
}

func Serve(ctx context.Context, opts Options) (baseURL string, stop func() error, err error) {
	if opts.NewHandler == nil {
		return "", nil, fmt.Errorf("NewHandler is nil")
	}
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		return "", nil, fmt.Errorf("web server must bind to 127.0.0.1")
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return "", nil, fmt.Errorf("token is required")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}

	// One live connection per project. The core's MCP host binds to a single
	// requester (internal/core/rpc_handler.go:46-52), so a second client on the
	// SAME project would silently take over its prompts. Different projects have
	// different cores, so they do not contend — the guard is keyed, not global.
	//
	// Each slot remembers how to end its connection, because closing a project
	// through the API must drop that project's socket *before* the core is
	// closed: the handler goroutines dereference the core's tools and
	// jsonrpc.Server does not recover panics, so a request arriving on a closed
	// core would take the whole server down.
	var busyMu sync.Mutex
	live := map[string]*liveConn{}
	acquire := func(key string, cancel context.CancelFunc) (*liveConn, bool) {
		busyMu.Lock()
		defer busyMu.Unlock()
		if _, taken := live[key]; taken {
			return nil, false
		}
		lc := &liveConn{cancel: cancel, done: make(chan struct{})}
		live[key] = lc
		return lc, true
	}
	release := func(key string, lc *liveConn) {
		busyMu.Lock()
		if live[key] == lc {
			delete(live, key)
		}
		busyMu.Unlock()
		close(lc.done)
	}
	// evict ends the live connection for key, if any, and waits up to wait for
	// its handler to return — which happens only after jsonrpc.Server has waited
	// for every in-flight request. It returns the connection's done channel
	// (already closed when settled is true, nil when there was no connection)
	// so a caller that gave up waiting can still act once it closes.
	evict := func(key string, wait time.Duration) (settled bool, done <-chan struct{}) {
		busyMu.Lock()
		lc := live[key]
		busyMu.Unlock()
		if lc == nil {
			return true, nil
		}
		lc.cancel()
		select {
		case <-lc.done:
			return true, lc.done
		case <-time.After(wait):
			return false, lc.done
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", requireToken(token, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(opts.Health)
	}))
	mux.HandleFunc("/ws", requireToken(token, func(w http.ResponseWriter, r *http.Request) {
		projectID := strings.TrimSpace(r.URL.Query().Get("project"))

		// Resolve the handler factory before touching the guard, so a bad
		// project id never occupies a slot.
		newHandler := opts.NewHandler
		var projectCore *core.Core
		if projectID != "" {
			if opts.Registry == nil {
				http.Error(w, "project routing is not enabled", http.StatusNotFound)
				return
			}
			c, ok := opts.Registry.Get(projectID)
			if !ok {
				http.Error(w, "project_not_open", http.StatusNotFound)
				return
			}
			if opts.NewProjectHandler == nil {
				http.Error(w, "project routing is not configured", http.StatusNotFound)
				return
			}
			projectCore = c
			newHandler = func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
				return opts.NewProjectHandler(c)
			}
		}
		if newHandler == nil {
			http.Error(w, "no handler", http.StatusNotFound)
			return
		}

		connCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		guardKey := projectID // "" is the project-less default connection
		lc, ok := acquire(guardKey, cancel)
		if !ok {
			http.Error(w, "a client is already connected", http.StatusConflict)
			return
		}
		// Registered before p.Close and cancel run (defers are LIFO), so done
		// closes only once the socket is shut and Serve has returned.
		defer release(guardKey, lc)

		// Re-validate now that the slot is ours: a DELETE that ran between the
		// Get above and acquire has detached the project (Detach precedes
		// evict), so the core we hold must not be served.
		if projectID != "" {
			if cur, ok := opts.Registry.Get(projectID); !ok || cur != projectCore {
				http.Error(w, "project_not_open", http.StatusNotFound)
				return
			}
		}

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Same-origin only: the page is served by this very server.
			OriginPatterns: nil,
		})
		if err != nil {
			return
		}
		c.SetReadLimit(jsonrpc.DefaultMaxContentBytes)

		p := NewPipe(connCtx, c)
		defer func() { _ = p.Close() }()

		// Cancel the connection's context the moment the socket dies, rather
		// than after Serve returns: Serve waits for its in-flight handlers, and
		// a handler blocked in Server.Request waits on a context derived from
		// connCtx — cancelling only after Serve returns deadlocks the two. See
		// Pipe.Done.
		go func() {
			select {
			case <-p.Done():
				cancel()
			case <-connCtx.Done():
			}
		}()

		h, attach := newHandler()
		srv := jsonrpc.NewServer(h, p.Reader(), p.Writer())
		if attach != nil {
			attach(srv)
		}
		// Serve returns when the socket closes (io.EOF) or ctx is done. Every
		// pending server-initiated request unblocks with the cancelled ctx —
		// which is what makes a closed tab fail permission requests closed.
		_ = srv.Serve(connCtx)
	}))

	if opts.Registry != nil {
		// requireToken then requireSameOrigin: the cookie is SameSite=Strict,
		// but on loopback "site" ignores the port, so a page on 127.0.0.1:<other>
		// could still post here with the cookie attached. The Origin header,
		// when a browser sends one, must name this server.
		api := func(next http.HandlerFunc) http.HandlerFunc {
			return requireToken(token, requireSameOrigin(next))
		}
		mux.HandleFunc("/api/projects", api(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				writeJSONStatus(w, http.StatusOK, map[string]any{"projects": opts.Registry.List()})
			case http.MethodPost:
				handleOpenProject(ctx, w, r, opts)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}))
		mux.HandleFunc("/api/projects/", api(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
			// Detach first, so no dial that starts from here on can obtain the
			// core; then drop the tab (the client sees a normal disconnect); and
			// only when its handler has returned — nothing can be running on the
			// core any more — close the core. Order matters: closing while a
			// handler is live would nil the core's tools under it.
			c, err := opts.Registry.Detach(id)
			if err != nil {
				writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "project_not_open"})
				return
			}
			if c == nil { // an errored placeholder: no core, no socket
				w.WriteHeader(http.StatusNoContent)
				return
			}
			settled, done := evict(id, 5*time.Second)
			if settled {
				_ = c.Close()
			} else {
				// The connection did not wind down in time. The project is
				// already gone from the registry; its core closes the moment
				// the connection finally ends, not before. Accepted: such a
				// core is no longer reachable by Registry.Shutdown, so at
				// process exit it closes when the server ctx ends the
				// connection — possibly after runWeb has returned.
				go func() { <-done; _ = c.Close() }()
			}
			w.WriteHeader(http.StatusNoContent)
		}))
	}

	if opts.Assets != nil {
		files := http.FileServer(http.FS(opts.Assets))
		mux.Handle("/", requireToken(token, func(w http.ResponseWriter, r *http.Request) {
			// A valid token in the query means "this is the first load": hand
			// over the cookie and bounce to the same path without it, so the
			// credential leaves the address bar and the history.
			if strings.TrimSpace(r.URL.Query().Get("token")) == token {
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				q := r.URL.Query()
				q.Del("token")
				target := r.URL.Path
				if enc := q.Encode(); enc != "" {
					target += "?" + enc
				}
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
			files.ServeHTTP(w, r)
		}))
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	stop = func() error {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		_ = ln.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		_ = stop()
	}()

	return "http://" + ln.Addr().String(), stop, nil
}

// liveConn is one occupied guard slot: how to end that connection and when
// its handler has actually returned.
type liveConn struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// requireSameOrigin rejects a request whose Origin header names another
// origin. Browsers send Origin on cross-origin fetches and on every POST;
// curl and scripts send none and pass. This is the CSRF guard the cookie's
// SameSite=Strict cannot be on loopback, where every 127.0.0.1:<port> is one
// "site".
func requireSameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if !strings.EqualFold(origin, "http://"+r.Host) && !strings.EqualFold(origin, "https://"+r.Host) {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

// requireToken accepts the same two forms core --http accepts
// (protocol/jsonrpc/http.go:142-153), plus ?token= for the WebSocket handshake:
// the browser WebSocket API cannot set request headers.
func requireToken(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func authorized(r *http.Request, token string) bool {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(h), "bearer ") &&
		strings.TrimSpace(h[len("bearer "):]) == token {
		return true
	}
	if strings.TrimSpace(r.Header.Get("X-Orchestra-Token")) == token {
		return true
	}
	if c, err := r.Cookie(cookieName); err == nil && strings.TrimSpace(c.Value) == token {
		return true
	}
	return strings.TrimSpace(r.URL.Query().Get("token")) == token
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleOpenProject implements POST /api/projects. The status codes and error
// strings are the published contract — see the spec's "The contract".
func handleOpenProject(ctx context.Context, w http.ResponseWriter, r *http.Request, opts Options) {
	var req struct {
		Path string `json:"path"`
		Init bool   `json:"init"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}

	p, err := opts.Registry.Open(ctx, req.Path)
	if err == nil {
		writeJSONStatus(w, http.StatusCreated, p)
		return
	}

	// An uninitialised directory is initialised only when asked, then reopened.
	if errors.Is(err, projects.ErrNotInitialized) && req.Init && opts.InitProject != nil {
		if ierr := opts.InitProject(ctx, req.Path); ierr != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
				"error": "open_failed", "path": req.Path, "detail": ierr.Error(),
			})
			return
		}
		p, err = opts.Registry.Open(ctx, req.Path)
		if err == nil {
			writeJSONStatus(w, http.StatusCreated, p)
			return
		}
	}

	switch {
	case errors.Is(err, projects.ErrAlreadyOpen):
		id, _ := opts.Registry.OpenedIDFor(req.Path)
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "already_open", "id": id})
	case errors.Is(err, projects.ErrNoSuchDir):
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "no_such_dir", "path": req.Path})
	case errors.Is(err, projects.ErrNotInitialized):
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"error": "not_initialized", "path": req.Path})
	default:
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
			"error": "open_failed", "path": req.Path, "detail": err.Error(),
		})
	}
}
