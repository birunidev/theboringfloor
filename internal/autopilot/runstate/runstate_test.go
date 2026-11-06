package runstate

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tmpPaths(t *testing.T) Paths {
	t.Helper()
	p := Paths{Root: filepath.Join(t.TempDir(), "ap")}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKillSwitch(t *testing.T) {
	p := tmpPaths(t)
	if p.Stopped() || p.CheckStop() != nil {
		t.Fatal("fresh root must not be stopped")
	}
	if err := p.SetStop("manual from TUI", time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(p.CheckStop(), ErrStopped) {
		t.Fatal("want ErrStopped")
	}
	body, _ := os.ReadFile(p.StopFile())
	if !strings.Contains(string(body), "manual from TUI") {
		t.Fatalf("stop reason missing: %q", body)
	}
	if err := p.ClearStop(); err != nil || p.Stopped() {
		t.Fatalf("clear: %v stopped=%v", err, p.Stopped())
	}
	if err := p.ClearStop(); err != nil {
		t.Fatalf("double clear: %v", err)
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestBudgetCaps(t *testing.T) {
	p := tmpPaths(t)
	c := &clock{t: time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)}
	b, err := OpenBudget(p.UsageFile(), 100, 250, c.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Charge("BII-1", 60); err != nil {
		t.Fatal(err)
	}
	// Crossing the per-card cap reports it, and still records the tokens.
	if err := b.Charge("BII-1", 50); !errors.Is(err, ErrCardCap) {
		t.Fatalf("want ErrCardCap, got %v", err)
	}
	if got := b.Snapshot().Cards["BII-1"]; got != 110 {
		t.Fatalf("card tokens = %d", got)
	}
	if !errors.Is(b.Allow("BII-1"), ErrCardCap) || b.Allow("BII-2") != nil {
		t.Fatal("allow mismatch")
	}
	if err := b.Charge("BII-2", 99); err != nil {
		t.Fatal(err)
	}
	if err := b.Charge("BII-3", 50); !errors.Is(err, ErrDailyCap) {
		t.Fatalf("want ErrDailyCap, got %v", err)
	}
	// A restart reads the same ledger: caps survive.
	b2, err := OpenBudget(p.UsageFile(), 100, 250, c.now)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(b2.Allow("BII-9"), ErrDailyCap) {
		t.Fatal("daily cap lost across restart")
	}
	// Next day: daily resets, per-card lifetime total does not.
	c.t = c.t.Add(24 * time.Hour)
	if err := b2.Allow("BII-9"); err != nil {
		t.Fatalf("new day should allow: %v", err)
	}
	if !errors.Is(b2.Allow("BII-1"), ErrCardCap) {
		t.Fatal("per-card cap must persist across days")
	}
}

func TestBudgetCorruptLedgerRefuses(t *testing.T) {
	p := tmpPaths(t)
	if err := os.WriteFile(p.UsageFile(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBudget(p.UsageFile(), 1, 1, nil); err == nil {
		t.Fatal("a corrupt ledger must not silently reset the caps")
	}
}

func TestBudgetZeroCapDisabled(t *testing.T) {
	p := tmpPaths(t)
	b, _ := OpenBudget(p.UsageFile(), 0, 0, nil)
	if err := b.Charge("X", 1<<40); err != nil {
		t.Fatalf("zero caps should be disabled: %v", err)
	}
}

func TestLockSingleInstance(t *testing.T) {
	p := tmpPaths(t)
	l1, err := AcquireLock(p.LockFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(p.LockFile()); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: want ErrLocked, got %v", err)
	}
	if err := l1.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := AcquireLock(p.LockFile())
	if err != nil {
		t.Fatalf("relock after release: %v", err)
	}
	_ = l2.Release()
}

func TestRunLogAndStatus(t *testing.T) {
	p := tmpPaths(t)
	now := func() time.Time { return time.Date(2026, 11, 6, 1, 2, 3, 0, time.UTC) }
	l, err := p.OpenRunLog("BII-../655", NewRunID(now()), now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(l.Path())) != p.RunsDir() {
		t.Fatalf("log path escaped runs dir: %s", l.Path())
	}
	_ = l.Write(Event{Kind: "start", Msg: "claimed"})
	_ = l.Write(Event{Kind: "verdict", Stage: "review-ac", Round: 1, Data: map[string]any{"verdict": "fail"}})
	_ = l.Close()

	f, _ := os.Open(l.Path())
	defer f.Close()
	var events []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		events = append(events, e)
	}
	if len(events) != 2 || events[1].Card != "BII-../655" || events[1].Round != 1 || events[0].TS == "" {
		t.Fatalf("events = %+v", events)
	}
	if fi, _ := os.Stat(l.Path()); fi.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %v", fi.Mode().Perm())
	}

	if s, _ := p.ReadStatus(); s.State != "idle" {
		t.Fatalf("absent status should read idle, got %q", s.State)
	}
	if err := p.WriteStatus(Status{State: "running", Card: "BII-655", Stage: "preflight"}, now()); err != nil {
		t.Fatal(err)
	}
	s, err := p.ReadStatus()
	if err != nil || s.Card != "BII-655" || s.UpdatedAt == "" {
		t.Fatalf("status = %+v err=%v", s, err)
	}
}
