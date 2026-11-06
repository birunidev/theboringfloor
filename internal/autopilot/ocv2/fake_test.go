package ocv2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeV2 is a scripted opencode v2 server. After a prompt it raises the
// queued permission requests one at a time; once all are answered it adds
// tokens and finishes the turn with an assistant message and an idle.
type fakeV2 struct {
	t          *testing.T
	pw         string
	mu         sync.Mutex
	queue      []PermissionRequest
	pending    []PermissionRequest
	replies    map[string]string
	msgs       []map[string]any // newest first
	tokens     Tokens
	busy       bool
	answer     string
	outcome    string
	never      bool // never finish (timeout tests)
	perTick    int64
	created    map[string]any
	interrupts int
	seq        int
}

func newFake(t *testing.T) (*fakeV2, *httptest.Server) {
	f := &fakeV2{t: t, pw: "pw", replies: map[string]string{}, answer: "done", outcome: "succeeded"}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeV2) id(p string) string {
	f.seq++
	return p + "_" + strings.Repeat("0", 3) + string(rune('a'+f.seq))
}

func (f *fakeV2) write(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
}

func (f *fakeV2) serve(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != "opencode" || p != f.pw {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/api/session":
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		f.write(w, map[string]any{"id": "ses_1"})
	case r.Method == http.MethodPost && path == "/api/session/ses_1/prompt":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.msgs = append([]map[string]any{{"id": f.id("msg"), "type": "user", "text": body["text"]}}, f.msgs...)
		f.busy = true
		f.write(w, map[string]any{"id": "u"})
	case r.Method == http.MethodGet && path == "/api/session/ses_1/permission":
		f.tick()
		f.write(w, f.pending)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/session/ses_1/permission/"):
		rid := strings.TrimSuffix(strings.TrimPrefix(path, "/api/session/ses_1/permission/"), "/reply")
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.replies[rid] = body["decision"]
		for i, p := range f.pending {
			if p.ID == rid {
				f.pending = append(f.pending[:i], f.pending[i+1:]...)
				break
			}
		}
		f.write(w, true)
	case r.Method == http.MethodGet && path == "/api/session/ses_1":
		f.write(w, map[string]any{"id": "ses_1", "tokens": f.tokens})
	case r.Method == http.MethodGet && path == "/api/session/ses_1/message":
		f.write(w, f.msgs)
	case r.Method == http.MethodPost && path == "/api/session/ses_1/interrupt":
		f.interrupts++
		f.busy = false
		f.write(w, true)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// tick advances the scripted turn by one step.
func (f *fakeV2) tick() {
	if !f.busy {
		return
	}
	f.tokens.Input += f.perTick
	if len(f.pending) > 0 {
		return
	}
	if len(f.queue) > 0 {
		f.pending = append(f.pending, f.queue[0])
		f.queue = f.queue[1:]
		return
	}
	if f.never {
		return
	}
	f.tokens.Output += 10
	f.msgs = append([]map[string]any{
		{"id": f.id("idle"), "type": "idle", "outcome": f.outcome},
		{"id": f.id("asst"), "type": "assistant", "finish": "stop", "content": []map[string]string{{"type": "reasoning", "text": ""}, {"type": "text", "text": f.answer}}},
	}, f.msgs...)
	f.busy = false
}
