package llmauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/authstore"
	"github.com/orchestra/orchestra/llm"
	"golang.org/x/oauth2"
)

// loginTimeout bounds an interactive login that the caller left unbounded.
// Five minutes is long enough for a human to find the browser window and
// short enough that a forgotten command does not sit forever.
const loginTimeout = 5 * time.Minute

// LoginConfig configures one interactive login.
type LoginConfig struct {
	// Name is the provider key from .orchestra.yml; the token is stored
	// under it and read back by TokenSourceFor.
	Name string
	// OAuth carries the endpoints and client identity from the user's config.
	OAuth llm.OAuthConfig
	// OpenURL opens the authorization URL. nil uses the real OS opener. It
	// must return as soon as the browser process starts and must NOT wait for
	// the page to load: Login has to reach its select before the loopback
	// handler can hand off, so a synchronous opener would deadlock against
	// its own server.
	OpenURL func(string) error
	// Out receives user-facing progress -- the device code, mostly.
	Out io.Writer
}

// oauth2Config builds the x/oauth2 config for one provider.
func oauth2Config(oc llm.OAuthConfig, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     strings.TrimSpace(oc.ClientID),
		ClientSecret: strings.TrimSpace(oc.ClientSecret),
		Endpoint: oauth2.Endpoint{
			AuthURL:       strings.TrimSpace(oc.AuthURL),
			TokenURL:      strings.TrimSpace(oc.TokenURL),
			DeviceAuthURL: strings.TrimSpace(oc.DeviceAuthURL),
			AuthStyle:     oauth2.AuthStyleInParams,
		},
		RedirectURL: redirectURL,
		Scopes:      oc.Scopes,
	}
}

// store persists a freshly issued grant along with what a later silent
// refresh needs: the token endpoint and the client identity.
func store(name string, oc llm.OAuthConfig, tok *oauth2.Token) error {
	return authstore.Save(Namespace, name, authstore.Token{
		TokenURL:     strings.TrimSpace(oc.TokenURL),
		ClientID:     strings.TrimSpace(oc.ClientID),
		ClientSecret: strings.TrimSpace(oc.ClientSecret),
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	})
}

// Login runs the configured interactive flow and persists the resulting
// token. It is the ONLY place in llmauth that opens a browser or prints a
// user code -- every other path fails with an actionable error instead.
func Login(ctx context.Context, cfg LoginConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("llmauth: provider name is required")
	}
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	// An interactive login waits on a human. If the caller set no deadline,
	// bound it: a user who closes the browser tab instead of approving would
	// otherwise leave the command hanging with nothing on screen. Same
	// pattern as DiscoverModelLimits (llm/limits.go:28).
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, loginTimeout)
		defer cancel()
	}
	switch cfg.OAuth.EffectiveFlow() {
	case "device":
		return loginDevice(ctx, cfg)
	default:
		return loginBrowser(ctx, cfg)
	}
}

func loginBrowser(ctx context.Context, cfg LoginConfig) error {
	openURL := cfg.OpenURL
	if openURL == nil {
		openURL = openBrowser
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("llmauth: open a local port for the OAuth redirect: %w", err)
	}
	redirectURL := fmt.Sprintf("http://%s/", ln.Addr().String())
	oc := oauth2Config(cfg.OAuth, redirectURL)

	verifier := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier() // any high-entropy one-shot string

	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
	}
	for k, v := range cfg.OAuth.ExtraParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	send := func(r result) {
		select {
		case results <- r:
		default: // first answer wins; later ones are noise
		}
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("state"); got != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			send(result{err: errors.New("llmauth: OAuth state mismatch")})
			return
		}
		fmt.Fprint(w, "Authentication successful. You can close this window.")
		send(result{code: r.URL.Query().Get("code")})
	})}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			send(result{err: err})
		}
	}()
	defer srv.Close()

	if err := openURL(oc.AuthCodeURL(state, opts...)); err != nil {
		return fmt.Errorf("llmauth: open browser: %w", err)
	}

	var res result
	select {
	case res = <-results:
	case <-ctx.Done():
		return ctx.Err()
	}
	if res.err != nil {
		return res.err
	}
	if res.code == "" {
		return errors.New("llmauth: authorization server returned no code")
	}

	tok, err := oc.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("provider %q: exchange authorization code: %w", cfg.Name, err)
	}
	return store(cfg.Name, cfg.OAuth, tok)
}

// Logout deletes the stored token for a provider. Idempotent.
func Logout(name string) error {
	return authstore.Delete(Namespace, name)
}

// TokenSourceFor returns a function yielding a valid bearer for name,
// refreshing silently and persisting the refreshed grant. It never prompts:
// when nothing is stored, or when a refresh fails, the error names the login
// command, because that error surfaces to the user through an ordinary chat
// request where a bare 401 would tell them nothing.
func TokenSourceFor(ctx context.Context, name string, oc llm.OAuthConfig) (func() (string, error), error) {
	tok, err := authstore.Load(Namespace, name)
	if err != nil {
		if errors.Is(err, authstore.ErrNoToken) {
			return nil, fmt.Errorf("provider %q: not authenticated, run: orchestra auth login %s", name, name)
		}
		return nil, err
	}

	conf := oauth2Config(oc, "")
	base := conf.TokenSource(ctx, &oauth2.Token{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	})
	ts := authstore.NewPersistingTokenSource(Namespace, name, base, tok)
	return func() (string, error) {
		t, err := ts.Token()
		if err != nil {
			return "", fmt.Errorf("provider %q: refresh failed, run: orchestra auth login %s: %w", name, name, err)
		}
		return t.AccessToken, nil
	}, nil
}

// openBrowser launches the OS default browser at url without waiting for it
// to exit -- see LoginConfig.OpenURL for why that matters. Not covered by
// tests: actually opening a browser cannot run in CI, which is exactly why
// OpenURL exists as a seam.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// loginDevice runs RFC 8628 device authorization: ask for a code, print it,
// then poll. It never opens a browser -- the whole point of this flow is that
// the machine running Orchestra has none, and the user approves on a second
// device.
//
// Polling cadence is delegated to x/oauth2's DeviceAccessToken, which honours
// the server's `interval` and backs off on `slow_down`. Inventing our own
// cadence here produces rate-limit errors that look like auth failures.
func loginDevice(ctx context.Context, cfg LoginConfig) error {
	oc := oauth2Config(cfg.OAuth, "")

	opts := make([]oauth2.AuthCodeOption, 0, len(cfg.OAuth.ExtraParams))
	for k, v := range cfg.OAuth.ExtraParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}

	da, err := oc.DeviceAuth(ctx, opts...)
	if err != nil {
		return fmt.Errorf("provider %q: request a device code: %w", cfg.Name, err)
	}

	target := da.VerificationURI
	if da.VerificationURIComplete != "" {
		target = da.VerificationURIComplete
	}
	fmt.Fprintf(cfg.Out, "Open %s and enter the code: %s\n", target, da.UserCode)

	tok, err := oc.DeviceAccessToken(ctx, da)
	if err != nil {
		return fmt.Errorf("provider %q: device authorization: %w", cfg.Name, err)
	}
	return store(cfg.Name, cfg.OAuth, tok)
}
