package spaceboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
)

func writeAuth(t *testing.T, expiresAt float64, serverURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp-auth.json")
	doc := map[string]any{
		"tebar-ai": map[string]any{"serverUrl": "https://api.tebar.app/mcp", "codeVerifier": "keep-me"},
		ServerName: map[string]any{
			"serverUrl":  serverURL,
			"clientInfo": map[string]any{"clientId": "client-123"},
			"tokens":     map[string]any{"accessToken": "old-access", "refreshToken": "old-refresh", "expiresAt": expiresAt},
		},
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTokenFreshIsReturnedAsIs(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	path := writeAuth(t, float64(now.Add(time.Hour).Unix()), SpaceURL)
	ts := NewTokenSource(path, "http://127.0.0.1:1/never")
	ts.now = func() time.Time { return now }
	tok, err := ts.Token(context.Background())
	if err != nil || tok != "old-access" {
		t.Fatalf("%q %v", tok, err)
	}
}

func TestTokenRefreshWritesBack(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = map[string]string{}
		for k := range r.PostForm {
			got[k] = r.PostForm.Get(k)
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 3600})
	}))
	defer srv.Close()
	path := writeAuth(t, float64(now.Add(30*time.Second).Unix()), SpaceURL)
	ts := NewTokenSource(path, srv.URL)
	ts.now = func() time.Time { return now }
	tok, err := ts.Token(context.Background())
	if err != nil || tok != "new-access" {
		t.Fatalf("%q %v", tok, err)
	}
	want := map[string]string{"grant_type": "refresh_token", "refresh_token": "old-refresh", "client_id": "client-123", "resource": SpaceURL}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("refresh form %s=%q, want %q", k, got[k], v)
		}
	}
	data, _ := os.ReadFile(path)
	var doc map[string]map[string]any
	json.Unmarshal(data, &doc)
	tokens := doc[ServerName]["tokens"].(map[string]any)
	if tokens["accessToken"] != "new-access" || tokens["refreshToken"] != "new-refresh" {
		t.Fatalf("rotated tokens not written back: %v", tokens)
	}
	if doc[ServerName]["serverUrl"] != SpaceURL || doc["tebar-ai"]["codeVerifier"] != "keep-me" {
		t.Fatal("write-back lost other fields or entries")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("auth file mode %v", fi.Mode().Perm())
	}
}

func TestTokenRefreshRejectedHidesBody(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","refresh_token":"SECRET-ECHO"}`))
	}))
	defer srv.Close()
	path := writeAuth(t, float64(now.Unix()), SpaceURL)
	ts := NewTokenSource(path, srv.URL)
	ts.now = func() time.Time { return now }
	_, err := ts.Token(context.Background())
	if !errors.Is(err, ErrNoLogin) || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "old-") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenWrongServerRefused(t *testing.T) {
	path := writeAuth(t, 9e12, "https://workspace.birunidev.com/mcp")
	if _, err := NewTokenSource(path, "").Token(context.Background()); err == nil || errors.Is(err, ErrNoLogin) {
		t.Fatalf("a login for another server must be refused explicitly, got %v", err)
	}
	if _, err := NewTokenSource(filepath.Join(t.TempDir(), "none.json"), "").Token(context.Background()); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("missing file: %v", err)
	}
}

type staticTokens struct {
	tok         atomic.Value
	invalidated atomic.Int32
}

func (s *staticTokens) Token(context.Context) (string, error) { return s.tok.Load().(string), nil }
func (s *staticTokens) Invalidate()                           { s.invalidated.Add(1); s.tok.Store("good") }

// fakeMCP answers initialize, the initialized notification and tools/call.
func fakeMCP(t *testing.T, sse bool, handle func(name string, args map[string]any) (any, bool)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-1")
			result = map[string]any{"protocolVersion": "2025-06-18"}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			args, _ := req.Params["arguments"].(map[string]any)
			v, isErr := handle(req.Params["name"].(string), args)
			text, _ := json.Marshal(v)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "isError": isErr}
		}
		resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("event: message\ndata: " + string(resp) + "\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	}))
}

func TestClientBoardOps(t *testing.T) {
	for _, sse := range []bool{false, true} {
		var calls []string
		srv := fakeMCP(t, sse, func(name string, args map[string]any) (any, bool) {
			calls = append(calls, name)
			switch name {
			case "list_cards":
				if args["cursor"] == nil {
					return map[string]any{"cards": []map[string]any{{"id": "c1", "key": "BII-1", "listId": "todo", "labelIds": []string{"lab"}}}, "nextCursor": "p2", "hasMore": true}, false
				}
				return map[string]any{"cards": []map[string]any{{"id": "c2", "key": "BII-2", "listId": "todo"}}, "hasMore": false}, false
			case "get_card":
				return map[string]any{"card": map[string]any{"id": "c1", "key": "BII-1", "comments": []map[string]any{{"id": "m1", "text": "hi"}}}}, false
			case "move_card", "add_comment":
				return map[string]any{"ok": true}, false
			case "get_board":
				return map[string]any{"board": map[string]any{"id": "b"}, "lists": []map[string]any{{"id": "l-done", "name": "Done"}, {"id": "l-todo", "name": "Todo"}}, "labels": []map[string]any{{"id": "lab", "name": "autopilot"}}}, false
			}
			return "no such tool", true
		})
		toks := &staticTokens{}
		toks.tok.Store("expired") // first call 401s, then Invalidate makes it good
		c := &Client{endpoint: srv.URL, tokens: toks, http: srv.Client()}
		ctx := context.Background()
		cards, err := c.ListCards(ctx, "b", "todo")
		if err != nil || len(cards) != 2 || !cards[0].HasLabel("lab") {
			t.Fatalf("sse=%v list: %+v %v", sse, cards, err)
		}
		if toks.invalidated.Load() != 1 {
			t.Fatalf("sse=%v: 401 did not trigger one refresh", sse)
		}
		card, err := c.GetCard(ctx, "BII-1")
		if err != nil || len(card.Comments) != 1 {
			t.Fatalf("get: %+v %v", card, err)
		}
		b, err := c.GetBoard(ctx, "b")
		if err != nil || b.ListID("done") != "l-done" || b.LabelID("AUTOPILOT") != "lab" || b.ListID("nope") != "" {
			t.Fatalf("board: %+v %v", b, err)
		}
		g := board.NewGuard(c, b.ListID("Done"))
		if err := g.MoveCard(ctx, "BII-1", "l-done", "k"); !errors.Is(err, board.ErrForbiddenMove) {
			t.Fatalf("guard let a Done move through: %v", err)
		}
		if err := g.MoveCard(ctx, "BII-1", "l-todo", "k"); err != nil {
			t.Fatal(err)
		}
		if err := c.AddComment(ctx, "BII-1", "hello", "k2"); err != nil {
			t.Fatal(err)
		}
		if err := c.CallTool(ctx, "bogus", nil, nil); err == nil {
			t.Fatal("isError result must be an error")
		}
		for _, n := range calls {
			if n == "move_card" {
				goto ok
			}
		}
		t.Fatal("allowed move never reached the server")
	ok:
		srv.Close()
	}
}

func TestEndpointIsPinned(t *testing.T) {
	if c := New(NewTokenSource("x", "")); c.endpoint != "https://space.birunidev.com/mcp" {
		t.Fatalf("endpoint = %s", c.endpoint)
	}
}
