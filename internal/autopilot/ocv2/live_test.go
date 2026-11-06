package ocv2

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveSmoke runs one real turn against the installed opencode. It is
// skipped unless AUTOPILOT_LIVE=1 because it spends model tokens.
//
//	AUTOPILOT_LIVE=1 go test ./internal/autopilot/ocv2 -run TestLiveSmoke -v
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("AUTOPILOT_LIVE") != "1" {
		t.Skip("set AUTOPILOT_LIVE=1 to run against the real opencode")
	}
	home, _ := os.UserHomeDir()
	if err := CheckSpaceConfig(home); err != nil {
		t.Fatalf("space config: %v", err)
	}
	dir := filepath.Join(home, ".theboringfloor", "autopilot", "live-smoke")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := Spawn(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	model, _ := ParseModel("opencode-go/muse-spark-1.3-contributor")
	c := s.Client()
	sid, err := c.CreateSession(ctx, SessionSpec{Title: "autopilot-live-smoke", Dir: dir, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	deny := &Policy{Root: dir} // nothing allowed
	res, err := c.RunTurn(ctx, TurnSpec{SessionID: sid, Text: "Reply with exactly: OK", Approver: deny,
		OnEvent: func(e TurnEvent) {
			if e.Kind == "permission" {
				t.Logf("permission %s -> allow=%v (%s)", e.Request.Action, e.Decision.Allow, e.Decision.Reason)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("text=%q outcome=%s billable=%d", res.Text, res.Outcome, res.Tokens.Billable())
	if !strings.Contains(res.Text, "OK") || res.Tokens.Billable() == 0 {
		t.Fatalf("unexpected result %+v", res)
	}
}
