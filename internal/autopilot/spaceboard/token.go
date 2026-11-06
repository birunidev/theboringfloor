// Package spaceboard is the autopilot's Board backed by the Birunidev Space
// MCP server at https://space.birunidev.com/mcp, and nothing else.
//
// It reuses the OAuth login opencode v1 stores for the birunidev-space
// server (mcp-auth.json): the access token is sent only to that one URL,
// refreshed through the server's own token endpoint when near expiry, and
// the rotated tokens are written back to the same file so opencode's login
// keeps working. Token values are never logged or returned in errors.
package spaceboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SpaceURL is the only MCP endpoint this package will talk to.
const SpaceURL = "https://space.birunidev.com/mcp"

// ServerName is the opencode MCP server entry whose login is reused.
const ServerName = "birunidev-space"

// DefaultAuthFile is opencode v1's MCP login store (see the opencodev1 wrapper).
func DefaultAuthFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode-v1", "opencode", "mcp-auth.json")
}

type storedTokens struct {
	AccessToken  string  `json:"accessToken"`
	RefreshToken string  `json:"refreshToken,omitempty"`
	ExpiresAt    float64 `json:"expiresAt,omitempty"` // unix seconds
	Scope        string  `json:"scope,omitempty"`
}

// TokenSource hands out a valid bearer for SpaceURL.
type TokenSource struct {
	mu       sync.Mutex
	path     string
	tokenURL string
	http     *http.Client
	now      func() time.Time
	cached   storedTokens
	clientID string
	loaded   bool
	skew     time.Duration
}

// NewTokenSource reads the birunidev-space login from path. tokenURL is the
// OAuth token endpoint (default https://space.birunidev.com/api/oauth/token).
func NewTokenSource(path, tokenURL string) *TokenSource {
	if tokenURL == "" {
		tokenURL = "https://space.birunidev.com/api/oauth/token"
	}
	return &TokenSource{path: path, tokenURL: tokenURL, http: &http.Client{Timeout: 30 * time.Second}, now: time.Now, skew: 2 * time.Minute}
}

// ErrNoLogin: opencode has no usable birunidev-space login.
var ErrNoLogin = errors.New("spaceboard: no birunidev-space login; run: opencodev1 mcp auth birunidev-space")

func (ts *TokenSource) load() (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(ts.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoLogin
		}
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("spaceboard: %s is not valid JSON", ts.path)
	}
	raw, ok := all[ServerName]
	if !ok {
		return nil, ErrNoLogin
	}
	var e struct {
		Tokens     *storedTokens `json:"tokens"`
		ServerURL  string        `json:"serverUrl"`
		ClientInfo struct {
			ClientID string `json:"clientId"`
		} `json:"clientInfo"`
	}
	if err := json.Unmarshal(raw, &e); err != nil || e.Tokens == nil || e.Tokens.AccessToken == "" {
		return nil, ErrNoLogin
	}
	if e.ServerURL != SpaceURL {
		return nil, fmt.Errorf("spaceboard: stored login is for %q, not %s", e.ServerURL, SpaceURL)
	}
	ts.cached, ts.clientID, ts.loaded = *e.Tokens, e.ClientInfo.ClientID, true
	return all, nil
}

func (ts *TokenSource) expiresSoon() bool {
	if ts.cached.ExpiresAt == 0 {
		return false
	}
	exp := time.Unix(0, int64(ts.cached.ExpiresAt*float64(time.Second)))
	return ts.now().Add(ts.skew).After(exp)
}

// Token returns a bearer valid for at least the skew window.
func (ts *TokenSource) Token(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	// Always re-read: opencode may have refreshed the login meanwhile.
	all, err := ts.load()
	if err != nil {
		return "", err
	}
	if !ts.expiresSoon() {
		return ts.cached.AccessToken, nil
	}
	if err := ts.refreshLocked(ctx, all); err != nil {
		return "", err
	}
	return ts.cached.AccessToken, nil
}

// Invalidate forces a refresh on the next Token call (after a 401).
func (ts *TokenSource) Invalidate() {
	ts.mu.Lock()
	ts.skew = 100 * 365 * 24 * time.Hour
	ts.mu.Unlock()
}

func (ts *TokenSource) refreshLocked(ctx context.Context, all map[string]json.RawMessage) error {
	defer func() { ts.skew = 2 * time.Minute }()
	if ts.cached.RefreshToken == "" {
		return fmt.Errorf("%w (token expired and no refresh token)", ErrNoLogin)
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {ts.cached.RefreshToken},
		"resource":      {SpaceURL},
	}
	if ts.clientID != "" {
		form.Set("client_id", ts.clientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := ts.http.Do(req)
	if err != nil {
		return fmt.Errorf("spaceboard: token refresh: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		// Never echo the body: it may contain token material.
		return fmt.Errorf("%w (refresh rejected: http %d)", ErrNoLogin, res.StatusCode)
	}
	var tr struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
		Scope        string  `json:"scope"`
	}
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return errors.New("spaceboard: token refresh returned no access token")
	}
	next := ts.cached
	next.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		next.RefreshToken = tr.RefreshToken
	}
	if tr.ExpiresIn > 0 {
		next.ExpiresAt = float64(ts.now().Add(time.Duration(tr.ExpiresIn*float64(time.Second))).UnixMilli()) / 1000
	}
	if tr.Scope != "" {
		next.Scope = tr.Scope
	}
	if err := ts.writeBack(all, next); err != nil {
		return err
	}
	ts.cached = next
	return nil
}

// writeBack stores rotated tokens into the same file, preserving every
// other server's entry and every other field of this one.
func (ts *TokenSource) writeBack(all map[string]json.RawMessage, t storedTokens) error {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(all[ServerName], &entry); err != nil {
		return err
	}
	tok, _ := json.Marshal(t)
	entry["tokens"] = tok
	raw, _ := json.Marshal(entry)
	all[ServerName] = raw
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(ts.path), ".mcp-auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), ts.path)
}
