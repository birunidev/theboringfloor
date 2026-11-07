// Package ocv2 drives opencode v2 (`opencode serve`, the /api/* surface of
// opencode 2.x) for the autopilot: spawn a private server, open sessions
// with a per-session permission ruleset, prompt them, answer each
// permission request through a code-level Approver, count tokens, and
// interrupt on cap, timeout or kill switch.
//
// This is deliberately separate from internal/backend, which speaks the
// older v1 opencode API used by the TUI.
package ocv2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ModelRef selects the model, e.g. {ProviderID: "opencode-go", ID: "muse-spark-1.3-contributor"}.
type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

// ParseModel parses "provider/model".
func ParseModel(s string) (ModelRef, error) {
	p, m, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok || p == "" || m == "" {
		return ModelRef{}, fmt.Errorf("model %q: want provider/model", s)
	}
	return ModelRef{ProviderID: p, ID: m}, nil
}

// Rule is one permission rule. Effect is allow, deny or ask.
type Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// AskEverything is the only ruleset the autopilot uses: every action asks,
// and the Approver decides in code. Unknown actions are rejected there.
var AskEverything = []Rule{{Action: "*", Resource: "*", Effect: "ask"}}

// PermissionRequest is one pending permission request.
type PermissionRequest struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionID"`
	Action    string         `json:"action"`
	Resources []string       `json:"resources"`
	Metadata  map[string]any `json:"metadata"`
	Message   string         `json:"message,omitempty"`
}

// Input returns the tool input carried in metadata.input (MCP tools).
func (r PermissionRequest) Input() map[string]any {
	in, _ := r.Metadata["input"].(map[string]any)
	return in
}

// Tokens is a session's cumulative usage.
type Tokens struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

// Billable is the count charged against caps: everything except cache reads.
func (t Tokens) Billable() int64 { return t.Input + t.Output + t.Reasoning + t.Cache.Write }

// Message is one session message; only the fields the autopilot reads.
type Message struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // user, assistant, idle, system
	Outcome string `json:"outcome,omitempty"`
	Finish  string `json:"finish,omitempty"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content,omitempty"`
}

// Text is the concatenated text parts of an assistant message.
func (m Message) Text() string {
	var b strings.Builder
	for _, p := range m.Content {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// Client talks to one opencode v2 server.
type Client struct {
	Base     string
	Password string
	HTTP     *http.Client
}

// NewClient returns a client for base with basic-auth password.
func NewClient(base, password string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Password: password, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rdr)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", c.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &HTTPError{Method: method, Path: path, Status: res.StatusCode, Body: trim(string(data), 300)}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("<")) {
		return fmt.Errorf("opencode %s %s: got HTML, not the v2 API (is this opencode 2.x?)", method, path)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("opencode %s %s: %w", method, path, err)
	}
	return json.Unmarshal(env.Data, out)
}

// HTTPError is a non-2xx answer from opencode.
type HTTPError struct {
	Method, Path string
	Status       int
	Body         string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("opencode %s %s: http %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// IsNotFound reports an opencode 404 (e.g. a permission request that is no
// longer pending because its turn already ended).
func IsNotFound(err error) bool {
	var h *HTTPError
	return errors.As(err, &h) && h.Status == http.StatusNotFound
}

// SessionSpec is what CreateSession needs.
type SessionSpec struct {
	Title string
	Dir   string
	Model ModelRef
	Rules []Rule
	Agent string // optional, e.g. "plan" or "build"
}

// CreateSession opens a session rooted at spec.Dir with spec.Rules.
func (c *Client) CreateSession(ctx context.Context, spec SessionSpec) (string, error) {
	if len(spec.Rules) == 0 {
		spec.Rules = AskEverything
	}
	body := map[string]any{
		"title":       spec.Title,
		"location":    map[string]string{"directory": spec.Dir},
		"model":       spec.Model,
		"permissions": spec.Rules,
	}
	if spec.Agent != "" {
		body["agent"] = spec.Agent
	}
	var s struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/session", body, &s); err != nil {
		return "", err
	}
	if s.ID == "" {
		return "", fmt.Errorf("opencode: session create returned no id")
	}
	return s.ID, nil
}

func sp(id string) string { return "/api/session/" + url.PathEscape(id) }

// Prompt sends text to the session.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) error {
	return c.do(ctx, http.MethodPost, sp(sessionID)+"/prompt", map[string]any{"text": text}, nil)
}

// Active returns the running sessions (id -> state type).
func (c *Client) Active(ctx context.Context) (map[string]string, error) {
	var raw map[string]struct {
		Type string `json:"type"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/session/active", nil, &raw); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range raw {
		out[k] = v.Type
	}
	return out, nil
}

// Permissions lists the session's pending permission requests.
func (c *Client) Permissions(ctx context.Context, sessionID string) ([]PermissionRequest, error) {
	var out []PermissionRequest
	return out, c.do(ctx, http.MethodGet, sp(sessionID)+"/permission", nil, &out)
}

// Reply answers a permission request: "once" or "reject". "always" is never
// sent: a saved grant would outlive this run's checks.
func (c *Client) Reply(ctx context.Context, sessionID, requestID, decision, message string) error {
	if decision != "once" && decision != "reject" {
		return fmt.Errorf("ocv2: refusing permission decision %q", decision)
	}
	body := map[string]any{"decision": decision}
	if message != "" {
		body["message"] = message
	}
	return c.do(ctx, http.MethodPost, sp(sessionID)+"/permission/"+url.PathEscape(requestID)+"/reply", body, nil)
}

// Interrupt stops the session's current turn.
func (c *Client) Interrupt(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodPost, sp(sessionID)+"/interrupt", map[string]any{}, nil)
}

// SessionTokens returns the session's cumulative token usage.
func (c *Client) SessionTokens(ctx context.Context, sessionID string) (Tokens, error) {
	var s struct {
		Tokens Tokens `json:"tokens"`
	}
	return s.Tokens, c.do(ctx, http.MethodGet, sp(sessionID), nil, &s)
}

// Messages returns the session's messages, newest first as the server
// returns them.
func (c *Client) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	var out []Message
	return out, c.do(ctx, http.MethodGet, sp(sessionID)+"/message", nil, &out)
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
