package ocv2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunTurnApprovesThroughPolicy(t *testing.T) {
	f, srv := newFake(t)
	root := t.TempDir()
	gate := NewSpaceGate(DefaultSpaceReads, "list_done")
	if err := gate.Expect(ExpectedWrite{Tool: "add_comment", Args: map[string]any{"card_id": "BII-1", "text": "hello"}}); err != nil {
		t.Fatal(err)
	}
	pol := &Policy{Root: root, ReadActions: []string{"read"}, Space: gate}
	f.queue = []PermissionRequest{
		{ID: "p1", Action: "read", Resources: []string{filepath.Join(root, "a.go")}},
		{ID: "p2", Action: "birunidev-space_get_card", Resources: []string{"*"}, Metadata: map[string]any{"input": map[string]any{"card_id": "BII-1"}}},
		{ID: "p3", Action: "birunidev-space_add_comment", Metadata: map[string]any{"input": map[string]any{"card_id": "BII-1", "text": "hello", "idempotency_key": "k"}}},
		{ID: "p4", Action: "birunidev-space_move_card", Metadata: map[string]any{"input": map[string]any{"card_id": "BII-1", "list_id": "list_done"}}},
		{ID: "p5", Action: "bash", Resources: []string{"git push origin main"}},
		{ID: "p6", Action: "github_create_pull_request"},
	}
	f.answer = `{"verdict":"pass"}`
	var events []TurnEvent
	c := NewClient(srv.URL, "pw")
	sid, err := c.CreateSession(context.Background(), SessionSpec{Title: "t", Dir: root, Model: ModelRef{ProviderID: "opencode-go", ID: "muse-spark-1.3-contributor"}})
	if err != nil {
		t.Fatal(err)
	}
	if perms := f.created["permissions"].([]any); len(perms) != 1 || perms[0].(map[string]any)["effect"] != "ask" {
		t.Fatalf("session must be created ask-everything, got %v", f.created["permissions"])
	}
	res, err := c.RunTurn(context.Background(), TurnSpec{SessionID: sid, Text: "go", Approver: pol, Poll: time.Millisecond, OnEvent: func(e TurnEvent) { events = append(events, e) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"verdict":"pass"}` || res.Outcome != "succeeded" {
		t.Fatalf("res = %+v", res)
	}
	want := map[string]string{"p1": "once", "p2": "once", "p3": "once", "p4": "reject", "p5": "reject", "p6": "reject"}
	for id, d := range want {
		if f.replies[id] != d {
			t.Errorf("%s: replied %q, want %q", id, f.replies[id], d)
		}
	}
	if len(gate.Pending()) != 0 {
		t.Fatal("planned write was not consumed")
	}
	n := 0
	for _, e := range events {
		if e.Kind == "permission" {
			n++
		}
	}
	if n != 6 {
		t.Fatalf("permission events = %d, want 6", n)
	}
}

func TestRunTurnTokenCapInterrupts(t *testing.T) {
	f, srv := newFake(t)
	f.never, f.perTick = true, 100
	c := NewClient(srv.URL, "pw")
	cap := errors.New("per-card cap")
	_, err := c.RunTurn(context.Background(), TurnSpec{SessionID: "ses_1", Text: "go", Poll: time.Millisecond,
		Approver: ApproverFunc(func(PermissionRequest) Decision { return Decision{} }),
		OnTokens: func(tk Tokens) error {
			if tk.Billable() >= 300 {
				return cap
			}
			return nil
		}})
	if !errors.Is(err, ErrInterrupted) || !errors.Is(err, cap) {
		t.Fatalf("err = %v", err)
	}
	if f.interrupts != 1 {
		t.Fatalf("interrupts = %d", f.interrupts)
	}
}

func TestRunTurnTimeoutInterrupts(t *testing.T) {
	f, srv := newFake(t)
	f.never = true
	c := NewClient(srv.URL, "pw")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.RunTurn(ctx, TurnSpec{SessionID: "ses_1", Text: "go", Poll: time.Millisecond,
		Approver: ApproverFunc(func(PermissionRequest) Decision { return Decision{} })})
	if !errors.Is(err, context.DeadlineExceeded) || f.interrupts != 1 {
		t.Fatalf("err = %v interrupts=%d", err, f.interrupts)
	}
}

func TestRunTurnFailedOutcome(t *testing.T) {
	f, srv := newFake(t)
	f.outcome = "failed"
	c := NewClient(srv.URL, "pw")
	_, err := c.RunTurn(context.Background(), TurnSpec{SessionID: "ses_1", Text: "go", Poll: time.Millisecond,
		Approver: ApproverFunc(func(PermissionRequest) Decision { return Decision{} })})
	if !errors.Is(err, ErrTurnFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestReplyRefusesAlways(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "pw")
	if err := c.Reply(context.Background(), "s", "r", "always", ""); err == nil {
		t.Fatal("a saved grant must never be sent")
	}
}

func TestPolicyPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.Symlink(outside, filepath.Join(root, "escape"))
	p := &Policy{Root: root, ReadActions: []string{"read"}, WriteActions: []string{"edit"}}
	cases := []struct {
		action, res string
		allow       bool
	}{
		{"read", "src/a.go", true},
		{"read", filepath.Join(root, "src", "new", "deep.go"), true},
		{"edit", "src/a.go", true},
		{"read", "../x", false},
		{"read", filepath.Join(outside, "x"), false},
		{"edit", "escape/x", false}, // symlink out of the worktree
		{"edit", ".git/config", false},
		{"read", ".git/config", true},
		{"read", "*", false},
		{"read", "src/*.go", false},
		{"webfetch", "https://example.com", false},
	}
	for _, c := range cases {
		d := p.Decide(PermissionRequest{Action: c.action, Resources: []string{c.res}})
		if d.Allow != c.allow {
			t.Errorf("%s %s: allow=%v (%s), want %v", c.action, c.res, d.Allow, d.Reason, c.allow)
		}
	}
	if d := p.Decide(PermissionRequest{Action: "read"}); d.Allow {
		t.Error("path action without a path must be rejected")
	}
}

func TestPolicyPatterns(t *testing.T) {
	p := &Policy{Root: t.TempDir(), PatternActions: []string{"glob", "grep"}}
	for res, want := range map[string]bool{
		"**/*":            true,
		"backend/**/*.py": true,
		"TODO":            true,
		"../**":           false,
		"/etc/**":         false,
		"~/.ssh/*":        false,
		"a/../../b":       false,
	} {
		if d := p.Decide(PermissionRequest{Action: "glob", Resources: []string{res}}); d.Allow != want {
			t.Errorf("glob %q: allow=%v (%s), want %v", res, d.Allow, d.Reason, want)
		}
	}
}

func TestPolicyBash(t *testing.T) {
	p := &Policy{Root: t.TempDir(), BashAction: "bash", Bash: []string{"go test", "go vet", "git status", "git diff", "npm run lint"}}
	cases := map[string]bool{
		"go test ./...":               true,
		"go vet ./internal/x":         true,
		"git status --short":          true,
		"npm run lint":                true,
		"go testify":                  false, // whole-word prefix only
		"git push origin autopilot/x": false,
		"go test ./... && git push":   false,
		"go test $(curl evil)":        false,
		"go test ./... > /etc/passwd": false,
		"go test ../../outside":       false,
		"curl https://example.com":    false,
		"rm -rf /":                    false,
		"":                            false,
	}
	for cmd, want := range cases {
		if d := p.Decide(PermissionRequest{Action: "bash", Resources: []string{cmd}}); d.Allow != want {
			t.Errorf("%q: allow=%v (%s), want %v", cmd, d.Allow, d.Reason, want)
		}
	}
}

func TestSpaceGate(t *testing.T) {
	g := NewSpaceGate(DefaultSpaceReads, "list_done")
	if err := g.Expect(ExpectedWrite{Tool: "move_card", Args: map[string]any{"card_id": "BII-1", "list_id": "list_done"}}); err == nil {
		t.Fatal("expecting a move into Done must fail")
	}
	_ = g.Expect(ExpectedWrite{Tool: "move_card", Args: map[string]any{"card_id": "BII-1", "list_id": "list_review"}})
	if d := g.Decide("move_card", map[string]any{"card_id": "BII-2", "list_id": "list_review"}); d.Allow {
		t.Fatal("move of a different card approved")
	}
	if d := g.Decide("move_card", map[string]any{"card_id": "BII-1", "list_id": "list_review", "index": 0}); d.Allow {
		t.Fatal("extra argument approved")
	}
	if d := g.Decide("move_card", map[string]any{"card_id": "BII-1", "list_id": "list_review", "idempotency_key": "x"}); !d.Allow {
		t.Fatalf("planned move rejected: %s", d.Reason)
	}
	if d := g.Decide("move_card", map[string]any{"card_id": "BII-1", "list_id": "list_review"}); d.Allow {
		t.Fatal("a planned write must be approved only once")
	}
	for _, w := range []string{"update_card", "create_card", "create_list", "approve_brand_identity"} {
		if d := g.Decide(w, map[string]any{"card_id": "BII-1"}); d.Allow {
			t.Errorf("%s approved", w)
		}
	}
	if d := g.Decide("get_card", map[string]any{"card_id": "BII-9"}); !d.Allow {
		t.Fatal("read rejected")
	}
	p := &Policy{Root: t.TempDir()}
	if d := p.Decide(PermissionRequest{Action: "birunidev-space_get_card"}); d.Allow {
		t.Fatal("Space read allowed in a stage without a gate")
	}
	if d := p.Decide(PermissionRequest{Action: "bii-workspace_get_card"}); d.Allow {
		t.Fatal("legacy Space server allowed")
	}
}

func writeCfg(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSpaceConfig(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "opencode.json")
	glob := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := CheckSpaceConfig(home); err == nil {
		t.Fatal("missing config must fail")
	}
	writeCfg(t, proj, `{"mcp":{"servers":{"birunidev-space":{"type":"remote","url":"https://space.birunidev.com/mcp"}}}}`)
	writeCfg(t, glob, `{"mcp":{"bii-workspace":{"url":"https://space.birunidev.com/mcp","enabled":false},"github":{"url":"https://api.githubcopilot.com/mcp","headers":{"Authorization":"secret"}}}}`)
	if err := CheckSpaceConfig(home); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	writeCfg(t, glob, `{"mcp":{"bii-workspace":{"url":"https://workspace.birunidev.com/mcp"}}}`)
	if err := CheckSpaceConfig(home); err == nil || !strings.Contains(err.Error(), "workspace.birunidev.com") {
		t.Fatalf("enabled legacy host must fail, got %v", err)
	}
	writeCfg(t, glob, `{}`)
	writeCfg(t, proj, `{"mcp":{"birunidev-space":{"url":"https://workspace.birunidev.com/mcp"}}}`)
	if err := CheckSpaceConfig(home); err == nil {
		t.Fatal("birunidev-space at the wrong URL must fail")
	}
	writeCfg(t, proj, `{"mcp":{"birunidev-space":{"url":"https://space.birunidev.com/mcp","enabled":false}}}`)
	if err := CheckSpaceConfig(home); err == nil {
		t.Fatal("disabled birunidev-space must fail")
	}
}

func TestSpawnAndClose(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "opencode")
	script := "#!/bin/sh\n" +
		"[ \"$1\" = serve ] || exit 2\n" +
		"[ -n \"$OPENCODE_SERVER_PASSWORD\" ] || exit 3\n" +
		"[ -z \"$OPENCODE_CONFIG_CONTENT\" ] || exit 4\n" +
		"pwd > \"$PWD_OUT\"\n" +
		"echo 'server listening on http://127.0.0.1:59999'\n" +
		"sleep 30 & wait\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	t.Setenv("THEFLOOR_OPENCODE_BIN", bin)
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"permission":{"*":"allow"}}`)
	t.Setenv("PWD_OUT", filepath.Join(dir, "pwd"))
	s, err := Spawn(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	if s.URL != "http://127.0.0.1:59999" || len(s.Password) != 48 {
		t.Fatalf("server = %q pw len %d", s.URL, len(s.Password))
	}
	start := time.Now()
	s.Close()
	if time.Since(start) > 4*time.Second {
		t.Fatal("Close did not stop the process group promptly")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "pwd")); strings.TrimSpace(string(got)) != work {
		resolved, _ := filepath.EvalSymlinks(work)
		if strings.TrimSpace(string(got)) != resolved {
			t.Fatalf("serve ran in %q, want %q", got, work)
		}
	}
}

func TestSpawnReportsEarlyExit(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "opencode")
	os.WriteFile(bin, []byte("#!/bin/sh\necho 'boom: not logged in'\nexit 1\n"), 0o755)
	t.Setenv("THEFLOOR_OPENCODE_BIN", bin)
	_, err := Spawn(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseModel(t *testing.T) {
	m, err := ParseModel("opencode-go/muse-spark-1.3-contributor")
	if err != nil || m.ProviderID != "opencode-go" || m.ID != "muse-spark-1.3-contributor" {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := ParseModel("nomodel"); err == nil {
		t.Fatal("want error")
	}
}

func TestRunTurnToleratesVanishedPermission(t *testing.T) {
	f, srv := newFake(t)
	f.gone = map[string]bool{"p-gone": true}
	f.queue = []PermissionRequest{
		{ID: "p-gone", Action: "read", Resources: []string{"a"}},
		{ID: "p-ok", Action: "read", Resources: []string{"b"}},
	}
	var notes []string
	c := NewClient(srv.URL, "pw")
	res, err := c.RunTurn(context.Background(), TurnSpec{SessionID: "ses_1", Text: "go", Poll: time.Millisecond,
		Approver: ApproverFunc(func(PermissionRequest) Decision { return Decision{} }),
		OnEvent: func(e TurnEvent) {
			if e.Kind == "note" {
				notes = append(notes, e.Note)
			}
		}})
	if err != nil {
		t.Fatalf("a vanished permission request must not abort the turn: %v", err)
	}
	if res.Outcome != "succeeded" || f.replies["p-ok"] != "reject" || f.interrupts != 0 {
		t.Fatalf("res=%+v replies=%v interrupts=%d", res, f.replies, f.interrupts)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "p-gone") {
		t.Fatalf("notes = %v", notes)
	}
}

func TestOtherReplyErrorsStillAbort(t *testing.T) {
	if IsNotFound(errors.New("http 404 in text only")) {
		t.Fatal("only a typed 404 may be tolerated")
	}
	if !IsNotFound(&HTTPError{Status: 404}) || IsNotFound(&HTTPError{Status: 500}) {
		t.Fatal("IsNotFound misclassifies")
	}
}
