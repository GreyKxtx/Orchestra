// Copyright 2026 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package oauthtest is a trimmed copy of
// github.com/modelcontextprotocol/go-sdk's internal/oauthtest fixture
// (v1.7.0), for Orchestra's own OAuth tests. The original lives under the
// SDK's own internal/ and cannot be imported across module boundaries, so
// it is reproduced here rather than vendored via Go modules. Only what
// internal/mcpauth's tests exercise is kept: authorization_code and
// refresh_token grants, PKCE, dynamic client registration, and
// preregistered clients.
package oauthtest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type ClientInfo struct {
	Secret       string
	RedirectURIs []string
}

type RegistrationConfig struct {
	// PreregisteredClients is a map of valid ClientIDs to ClientSecrets.
	PreregisteredClients map[string]ClientInfo
	// Whether dynamic client registration is enabled.
	DynamicClientRegistrationEnabled bool
}

// Config holds configuration for FakeAuthorizationServer.
type Config struct {
	// RegistrationConfig configures client registration.
	RegistrationConfig *RegistrationConfig
	// AccessTokenTTL, if non-zero, is the expires_in (in seconds) returned
	// by the /token endpoint. Zero uses a default of 3600. Set it small to
	// force a reuse token source to treat the access token as expired.
	AccessTokenTTL int
	// IssueRefreshToken, if true, includes a refresh_token in token
	// responses and enables grant_type=refresh_token at /token.
	IssueRefreshToken bool
}

// testRefreshToken is the refresh token issued and accepted by the fake
// server when Config.IssueRefreshToken is set.
const testRefreshToken = "test_refresh_token"

func (s *FakeAuthorizationServer) accessTokenExpiresIn() int {
	if s.config.AccessTokenTTL != 0 {
		return s.config.AccessTokenTTL
	}
	return 3600
}

// FakeAuthorizationServer is a fake OAuth 2.0 Authorization Server for
// testing. It supports only the authorization code grant, PKCE
// verification, client tracking, and dynamic registration.
type FakeAuthorizationServer struct {
	server  *httptest.Server
	Mux     *http.ServeMux
	config  Config
	clients map[string]ClientInfo
	codes   map[string]codeInfo
}

type codeInfo struct {
	CodeChallenge string
	Scope         string
}

// NewFakeAuthorizationServer creates a new FakeAuthorizationServer.
func NewFakeAuthorizationServer(config Config) *FakeAuthorizationServer {
	s := &FakeAuthorizationServer{
		Mux:    http.NewServeMux(),
		config: config,
		codes:  make(map[string]codeInfo),
	}
	if config.RegistrationConfig != nil {
		s.clients = maps.Clone(config.RegistrationConfig.PreregisteredClients)
	}
	if s.clients == nil {
		s.clients = make(map[string]ClientInfo)
	}

	s.Mux.HandleFunc("/authorize", s.handleAuthorize)
	s.Mux.HandleFunc("/token", s.handleToken)
	s.Mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleMetadata)
	if config.RegistrationConfig != nil && config.RegistrationConfig.DynamicClientRegistrationEnabled {
		s.Mux.HandleFunc("/register", s.handleRegister)
	}
	s.server = httptest.NewUnstartedServer(s.Mux)

	return s
}

// Start starts the HTTP server and registers a cleanup function on t.
func (s *FakeAuthorizationServer) Start(t testing.TB) {
	s.server.Start()
	t.Cleanup(s.server.Close)
}

// URL returns the base URL of the server (Issuer).
func (s *FakeAuthorizationServer) URL() string {
	return s.server.URL
}

func (s *FakeAuthorizationServer) handleMetadata(w http.ResponseWriter, r *http.Request) {
	var registrationEndpoint string
	if s.config.RegistrationConfig != nil && s.config.RegistrationConfig.DynamicClientRegistrationEnabled {
		registrationEndpoint = s.URL() + "/register"
	}
	meta := &oauthex.AuthServerMeta{
		Issuer:                            s.URL(),
		AuthorizationEndpoint:             s.URL() + "/authorize",
		TokenEndpoint:                     s.URL() + "/token",
		RegistrationEndpoint:              registrationEndpoint,
		ResponseTypesSupported:            []string{"code"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic"},
		// Advertise RFC 9207 support: the authorize endpoint includes "iss".
		AuthorizationResponseIssParameterSupported: true,
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(meta)
}

func (s *FakeAuthorizationServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var metadata oauthex.ClientRegistrationMetadata
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		http.Error(w, "failed to parse request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	clientID := rand.Text()
	ci := ClientInfo{Secret: rand.Text(), RedirectURIs: metadata.RedirectURIs}
	s.clients[clientID] = ci
	metadata.TokenEndpointAuthMethod = "client_secret_basic"
	json.NewEncoder(w).Encode(&oauthex.ClientRegistrationResponse{
		ClientID:                   clientID,
		ClientSecret:               ci.Secret,
		ClientRegistrationMetadata: metadata,
	})
}

func (s *FakeAuthorizationServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID := r.URL.Query().Get("client_id")
	clientInfo, ok := s.clients[clientID]
	if !ok {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	if !slices.Contains(clientInfo.RedirectURIs, redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	codeChallenge := r.URL.Query().Get("code_challenge")
	if codeChallenge == "" {
		http.Error(w, "missing code_challenge", http.StatusBadRequest)
		return
	}
	code := rand.Text()
	s.codes[code] = codeInfo{CodeChallenge: codeChallenge, Scope: r.URL.Query().Get("scope")}

	state := r.URL.Query().Get("state")
	redirectURL := fmt.Sprintf("%s?code=%s&state=%s&iss=%s", redirectURI, code, state, url.QueryEscape(s.URL()))
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (s *FakeAuthorizationServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.authenticateClient(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		s.handleRefreshTokenGrant(w, r)
	default:
		http.Error(w, fmt.Sprintf("unsupported grant_type: %s", r.Form.Get("grant_type")), http.StatusBadRequest)
	}
}

func (s *FakeAuthorizationServer) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := r.Form.Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	ci, ok := s.codes[code]
	if !ok {
		http.Error(w, "unknown authorization code", http.StatusBadRequest)
		return
	}
	verifier := r.Form.Get("code_verifier")
	if verifier == "" {
		http.Error(w, "missing code_verifier", http.StatusBadRequest)
		return
	}
	sha := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sha[:]) != ci.CodeChallenge {
		http.Error(w, "PKCE verification failed", http.StatusBadRequest)
		return
	}
	resp := map[string]any{
		"access_token": "test_access_token",
		"token_type":   "Bearer",
		"expires_in":   s.accessTokenExpiresIn(),
	}
	if s.config.IssueRefreshToken {
		resp["refresh_token"] = testRefreshToken
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleRefreshTokenGrant implements grant_type=refresh_token, returning a
// distinct access token so callers can observe that a refresh occurred.
func (s *FakeAuthorizationServer) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	if !s.config.IssueRefreshToken {
		http.Error(w, "refresh_token grant not supported", http.StatusBadRequest)
		return
	}
	if r.Form.Get("refresh_token") != testRefreshToken {
		http.Error(w, "invalid refresh_token", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  "test_access_token_refreshed",
		"token_type":    "Bearer",
		"expires_in":    s.accessTokenExpiresIn(),
		"refresh_token": testRefreshToken,
	})
}

func (s *FakeAuthorizationServer) authenticateClient(r *http.Request) error {
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.Form.Get("client_id")
		clientSecret = r.Form.Get("client_secret")
	}
	clientInfo, ok := s.clients[clientID]
	if !ok || clientInfo.Secret != clientSecret {
		return errors.New("client not found")
	}
	return nil
}
