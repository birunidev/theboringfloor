package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
)

func useProfiles(e *Engine, profiles ...Profile) {
	e.Cfg.Boards[0].Profiles = profiles
}

func noBoardWrites(t *testing.T, fbComments map[string][]string, moves int, pubCalls int) {
	t.Helper()
	if len(fbComments) != 0 || moves != 0 || pubCalls != 0 {
		t.Fatalf("the board or the publisher was touched: comments=%v moves=%d publishes=%d", fbComments, moves, pubCalls)
	}
}

func TestCardWithoutAProfileIsNeverClaimed(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, nil)
	useProfiles(e, Profile{Name: "core-frontend", TitlePrefixes: []string{"[V2 FE"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}})

	if err := e.Tick(context.Background()); !errors.Is(err, ErrNothingToDo) {
		t.Fatalf("Tick = %v, want nothing to do (card has no profile)", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)

	// A direct RunCard cannot bypass the profile either.
	r, err := e.Resolve(context.Background(), e.Cfg.Boards[0])
	if err != nil {
		t.Fatal(err)
	}
	cands, _ := e.Candidates(context.Background(), r)
	if len(cands) != 1 {
		t.Fatalf("candidates = %d", len(cands))
	}
	if err := e.RunCard(context.Background(), r, cands[0]); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("RunCard = %v, want ErrNoProfile", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)
	if entries, _ := os.ReadDir(e.Paths.WorktreesDir()); len(entries) != 0 {
		t.Fatalf("a worktree was prepared for a refused card: %v", entries)
	}
}

func parallelLanes(e *Engine) {
	e.Cfg.Boards[0].ReadyList = "Todo"
	e.Cfg.Boards[0].Lanes = []Lane{{Name: "any", TitlePrefixes: []string{"make"}, WorkingList: "Review"}}
}

func TestParallelBatchFailsClosedWithoutAProfile(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, nil)
	parallelLanes(e)
	useProfiles(e, Profile{Name: "core-frontend", TitlePrefixes: []string{"[V2 FE"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}})
	err := e.TickParallel(context.Background())
	if err == nil || !errors.Is(err, ErrNoProfile) {
		t.Fatalf("TickParallel = %v, want ErrNoProfile", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)
}

func TestStopBlocksBothTickPathsBeforeAnyClaim(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, nil)
	parallelLanes(e)
	useProfiles(e, Profile{Name: "any", TitlePrefixes: []string{"make"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}})
	if err := e.Paths.SetStop("test", e.now()); err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(context.Background()); !errors.Is(err, runstate.ErrStopped) {
		t.Fatalf("Tick = %v", err)
	}
	if err := e.TickParallel(context.Background()); !errors.Is(err, runstate.ErrStopped) {
		t.Fatalf("TickParallel = %v", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)
}

func TestBudgetsGateTheClaim(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, nil)
	useProfiles(e, Profile{Name: "any", TitlePrefixes: []string{"make"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}})

	// Per-card lifetime cap spent on an earlier day still blocks the card.
	b, _ := runstate.OpenBudget(e.Paths.UsageFile(), 40000, 1000000, nil)
	if err := b.Charge("BII-1", 40000); err == nil {
		t.Fatal("charging the whole card cap should report the cap")
	}
	e.Budget = b
	if err := e.Tick(context.Background()); !errors.Is(err, ErrNothingToDo) {
		t.Fatalf("Tick with the card cap spent = %v", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)

	// Shared daily cap spent by other cards blocks every card.
	b2, _ := runstate.OpenBudget(filepath.Join(t.TempDir(), "usage.json"), 40000, 100000, nil)
	b2.Charge("BII-OTHER-1", 39000)
	b2.Charge("BII-OTHER-2", 39000)
	b2.Charge("BII-OTHER-3", 22000)
	e.Budget = b2
	if err := e.Tick(context.Background()); !errors.Is(err, ErrNothingToDo) {
		t.Fatalf("Tick with the daily cap spent = %v", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)
}

func TestMissingLockfileBlocksTheCardNeverReview(t *testing.T) {
	needBwrap(t)
	e, fb, pub, _ := setup(t, readyPre, allLists, func(dir string) {
		os.WriteFile(filepath.Join(dir, "backend", "x.py"), []byte("x = 2\n"), 0o644)
	})
	useProfiles(e, Profile{Name: "any", TitlePrefixes: []string{"make"}, RequireFiles: []string{"frontend/package-lock.json"},
		Checks: []Check{{Name: "ok", Argv: []string{"true"}}}})
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c, _ := fb.GetCard(context.Background(), "c1")
	if c.ListID != "l-hold" || pub.calls != 0 {
		t.Fatalf("card ended in %s with %d publishes; a failed profile precondition must block it", c.ListID, pub.calls)
	}
	last := fb.Comments["c1"][len(fb.Comments["c1"])-1]
	if !strings.Contains(last, "profile preconditions") || !strings.Contains(last, "package-lock.json") {
		t.Fatalf("outcome does not say why:\n%s", last)
	}
	for _, m := range fb.Moves {
		if m.ListID == "l-done" {
			t.Fatal("card moved to Done")
		}
	}
}

func TestProfileRunIncludesUntrackedFilesAndNeverMergesOrCompletes(t *testing.T) {
	needBwrap(t)
	e, fb, pub, _ := setup(t, readyPre, allLists, func(dir string) {
		os.WriteFile(filepath.Join(dir, "backend", "brand_new.py"), []byte("NEW_UNTRACKED_LINE = 1\n"), 0o644)
	})
	useProfiles(e, Profile{Name: "any", TitlePrefixes: []string{"make"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}})
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c, _ := fb.GetCard(context.Background(), "c1")
	if c.ListID != "l-review" || pub.calls != 1 {
		t.Fatalf("card ended in %s, publishes %d", c.ListID, pub.calls)
	}
	packet := fb.Comments["c1"][len(fb.Comments["c1"])-1]
	for _, want := range []string{"backend/brand_new.py", "NEW_UNTRACKED_LINE = 1", "HEAD SHA: abc123head", "git diff --check", "draft"} {
		if !strings.Contains(strings.ToLower(packet), strings.ToLower(want)) {
			t.Errorf("review packet missing %q", want)
		}
	}
	for _, m := range fb.Moves {
		if m.ListID == "l-done" {
			t.Fatal("card moved to Done")
		}
	}
}

func directCard(t *testing.T, e *Engine) (Resolved, board.Card) {
	t.Helper()
	r, err := e.Resolve(context.Background(), e.Cfg.Boards[0])
	if err != nil {
		t.Fatal(err)
	}
	cands, err := e.Candidates(context.Background(), r)
	if err != nil || len(cands) != 1 {
		t.Fatalf("candidates = %d, err %v", len(cands), err)
	}
	return r, cands[0]
}

func TestDirectRunCardCannotBypassStop(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, nil)
	r, c := directCard(t, e)
	if err := e.Paths.SetStop("test", e.now()); err != nil {
		t.Fatal(err)
	}
	if err := e.RunCard(context.Background(), r, c); !errors.Is(err, runstate.ErrStopped) {
		t.Fatalf("RunCard with STOP set = %v, want ErrStopped", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)
	if entries, _ := os.ReadDir(e.Paths.WorktreesDir()); len(entries) != 0 {
		t.Fatalf("a worktree was prepared while STOP was set: %v", entries)
	}
	if s, _ := e.Paths.ReadStatus(); s.State == "running" {
		t.Fatalf("status shows a run while STOP was set: %+v", s)
	}
}

func TestDirectRunCardCannotBypassTheBudgets(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, nil)
	r, c := directCard(t, e)

	card, _ := runstate.OpenBudget(e.Paths.UsageFile(), 40000, 1000000, nil)
	card.Charge("BII-1", 40000)
	e.Budget = card
	if err := e.RunCard(context.Background(), r, c); !errors.Is(err, runstate.ErrCardCap) {
		t.Fatalf("RunCard with the card cap spent = %v, want ErrCardCap", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)

	day, _ := runstate.OpenBudget(filepath.Join(t.TempDir(), "usage.json"), 40000, 100000, nil)
	day.Charge("BII-OTHER-1", 39000)
	day.Charge("BII-OTHER-2", 39000)
	day.Charge("BII-OTHER-3", 22000)
	e.Budget = day
	if err := e.RunCard(context.Background(), r, c); !errors.Is(err, runstate.ErrDailyCap) {
		t.Fatalf("RunCard with the daily cap spent = %v, want ErrDailyCap", err)
	}
	noBoardWrites(t, fb.Comments, len(fb.Moves), pub.calls)
}

func TestSetupRunsInTheSandboxAndCannotReadTheMembersHome(t *testing.T) {
	needBwrap(t)
	home, _, _ := homeDirs(t) // plants $HOME/.ssh/id_ed25519 = SECRET-KEY
	e, fb, pub, _ := setup(t, readyPre, allLists, func(dir string) {
		os.WriteFile(filepath.Join(dir, "backend", "x.py"), []byte("x = 2\n"), 0o644)
	})
	// Not valid under Validate (a shell), on purpose: this proves the *runner*
	// confines whatever setup does, not just the config validator.
	e.Cfg.Boards[0].Setup = []Check{{Name: "steal", Argv: []string{"sh", "-c", "cat " + filepath.Join(home, ".ssh", "id_ed25519")}}}
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c, _ := fb.GetCard(context.Background(), "c1")
	if c.ListID != "l-hold" || pub.calls != 0 {
		t.Fatalf("card ended in %s (publishes %d): a setup that cannot read its inputs must block the card", c.ListID, pub.calls)
	}
	all := strings.Join(fb.Comments["c1"], "\n")
	if strings.Contains(all, "SECRET-KEY") {
		t.Fatalf("the member's secret reached the board:\n%s", all)
	}
	if !strings.Contains(all, "setup") {
		t.Fatalf("outcome does not say setup failed:\n%s", all)
	}
}

func TestSetupDoesNotInheritTheMembersEnvironment(t *testing.T) {
	needBwrap(t)
	homeDirs(t)
	t.Setenv("OPENCODE_API_KEY", "super-secret-value")
	e, fb, pub, _ := setup(t, readyPre, allLists, func(dir string) {
		os.WriteFile(filepath.Join(dir, "backend", "x.py"), []byte("x = 2\n"), 0o644)
	})
	// Fails (and prints) only if the variable is visible inside setup.
	e.Cfg.Boards[0].Setup = []Check{{Name: "env", Argv: []string{"sh", "-c", `test -z "$OPENCODE_API_KEY" || { echo "LEAK:$OPENCODE_API_KEY"; exit 1; }`}}}
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if all := strings.Join(fb.Comments["c1"], "\n"); strings.Contains(all, "super-secret-value") {
		t.Fatalf("environment secret visible to setup:\n%s", all)
	}
	if c, _ := fb.GetCard(context.Background(), "c1"); c.ListID != "l-review" || pub.calls != 1 {
		t.Fatalf("a clean setup should let the card proceed: list %s, publishes %d", c.ListID, pub.calls)
	}
}
