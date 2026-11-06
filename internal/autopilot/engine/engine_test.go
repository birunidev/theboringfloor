package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/worktree"
)

type fakeSpace struct {
	*board.Fake
	info spaceboard.BoardInfo
}

func (f fakeSpace) GetBoard(context.Context, string) (spaceboard.BoardInfo, error) {
	return f.info, nil
}

type scriptAgent struct {
	pre    string
	review string
	edit   func(dir string)
	dir    string
}

func (a *scriptAgent) Run(_ context.Context, req pipeline.StageRequest) (pipeline.StageResponse, error) {
	switch req.Role {
	case pipeline.RolePreflight:
		return pipeline.StageResponse{Text: a.pre}, nil
	case pipeline.RoleImplement:
		if a.edit != nil {
			a.edit(a.dir)
		}
		return pipeline.StageResponse{Text: "done", Session: "s"}, nil
	}
	return pipeline.StageResponse{Text: a.review}, nil
}

type fakePub struct{ calls int }

func (p *fakePub) Publish(context.Context, string, string, string, string, string, string) (string, error) {
	p.calls++
	return "https://github.com/birunidev/birunidev-space/pull/99", nil
}

func gitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@e"}, {"GIT_COMMITTER_EMAIL", "t@e"}} {
		t.Setenv(kv[0], kv[1])
	}
	base := t.TempDir()
	run := func(dir string, args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	origin, seed, clone := filepath.Join(base, "o.git"), filepath.Join(base, "seed"), filepath.Join(base, "clone")
	run(base, "init", "-q", "--bare", "-b", "main", origin)
	run(base, "init", "-q", "-b", "main", seed)
	os.MkdirAll(filepath.Join(seed, "backend"), 0o755)
	os.WriteFile(filepath.Join(seed, "backend", "x.py"), []byte("x = 1\n"), 0o644)
	run(seed, "add", ".")
	run(seed, "commit", "-q", "-m", "init")
	run(seed, "remote", "add", "origin", origin)
	run(seed, "push", "-q", "origin", "main")
	run(base, "clone", "-q", origin, clone)
	return clone
}

const (
	readyPre    = `{"ready":true,"policy_block":"","acceptance_criteria":["x is 2"],"scope_paths":["backend/**"],"questions":[],"chatgpt_prompt":"","summary":"ok"}`
	notReadyPre = `{"ready":false,"policy_block":"Implementation remains PAUSED until BII-638","acceptance_criteria":[],"scope_paths":[],"questions":["Is BII-638 done?"],"chatgpt_prompt":"Help me spec BII-1","summary":"paused"}`
	passVerdict = `{"verdict":"pass","findings":[],"summary":"ok"}`
)

func setup(t *testing.T, pre string, lists []spaceboard.List, edit func(string)) (*Engine, *board.Fake, *fakePub, *scriptAgent) {
	t.Helper()
	repo := gitRepo(t)
	info := spaceboard.BoardInfo{ID: "b", Name: "V2 Engineering", Description: "policy text", Lists: lists,
		Labels: []spaceboard.Label{{ID: "lab", Name: "autopilot"}}}
	fb := board.NewFake(
		board.Card{ID: "c1", Key: "BII-1", BoardID: "b", ListID: "l-todo", Title: "make x 2", LabelIDs: []string{"lab"}},
		board.Card{ID: "c2", Key: "BII-2", BoardID: "b", ListID: "l-todo", Title: "unlabelled"},
	)
	paths := runstate.Paths{Root: filepath.Join(t.TempDir(), "ap")}
	paths.Ensure()
	budget, _ := runstate.OpenBudget(paths.UsageFile(), 0, 0, nil)
	cfg := Config{Boards: []BoardConfig{{BoardID: "b", Name: "V2 Engineering", RepoName: "space", RepoDir: repo}}}
	cfg.Defaults()
	agent := &scriptAgent{pre: pre, review: passVerdict, edit: edit}
	pub := &fakePub{}
	e := &Engine{Cfg: cfg, Paths: paths, Space: fakeSpace{Fake: fb, info: info}, Budget: budget,
		WT: worktree.Manager{Root: paths.WorktreesDir()}, Pub: pub,
		NewAgent: func(_ context.Context, dir string, _ board.Card, _ BoardConfig, done []string, _ *runstate.RunLog, _ func(string)) (pipeline.Agent, func(), error) {
			if len(done) != 1 || done[0] != "l-done" {
				t.Fatalf("agent not told the Done list: %v", done)
			}
			agent.dir = dir
			return agent, func() {}, nil
		}}
	return e, fb, pub, agent
}

var allLists = []spaceboard.List{{ID: "l-todo", Name: "Todo"}, {ID: "l-review", Name: "Review"}, {ID: "l-hold", Name: "On Hold"},
	{ID: "l-clarify", Name: "Needs Clarification"}, {ID: "l-done", Name: "Done"}}

func TestDryRunNeverWrites(t *testing.T) {
	e, fb, pub, _ := setup(t, notReadyPre, allLists, nil)
	res, err := e.DryRun(context.Background(), e.Cfg.Boards[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Card.Key != "BII-1" {
		t.Fatalf("dry run should only see the labelled card: %+v", res)
	}
	if res[0].Preflight.Ready || !strings.Contains(res[0].Would, "PAUSED until BII-638") || res[0].WouldList != "Needs Clarification" {
		t.Fatalf("result = %+v", res[0])
	}
	if len(fb.Moves) != 0 || len(fb.Comments) != 0 || pub.calls != 0 {
		t.Fatalf("dry run wrote: moves=%v comments=%v pub=%d", fb.Moves, fb.Comments, pub.calls)
	}
	if out, _ := exec.Command("git", "-C", e.Cfg.Boards[0].RepoDir, "branch", "--list", "autopilot/*").Output(); len(out) != 0 {
		t.Fatalf("dry run created a branch: %s", out)
	}
	// --card works without the label and still writes nothing.
	res, err = e.DryRun(context.Background(), e.Cfg.Boards[0], "BII-2")
	if err != nil || len(res) != 1 || res[0].Card.Key != "BII-2" || len(fb.Moves) != 0 {
		t.Fatalf("card dry run: %+v %v", res, err)
	}
}

func TestLiveReadyGoesToReviewNeverDone(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, func(dir string) {
		os.WriteFile(filepath.Join(dir, "backend", "x.py"), []byte("x = 2\n"), 0o644)
	})
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pub.calls != 1 {
		t.Fatalf("publish calls = %d", pub.calls)
	}
	c, _ := fb.GetCard(context.Background(), "c1")
	if c.ListID != "l-review" {
		t.Fatalf("card ended in %s, want Review", c.ListID)
	}
	for _, m := range fb.Moves {
		if m.ListID == "l-done" {
			t.Fatal("card was moved to Done")
		}
	}
	comments := fb.Comments["c1"]
	if len(comments) != 2 || !strings.Contains(comments[0], "AUTOPILOT CLAIMED") || !strings.Contains(comments[1], "AUTOPILOT HANDOFF") || !strings.Contains(comments[1], "/pull/99") {
		t.Fatalf("comments = %q", comments)
	}
	if c2, _ := fb.GetCard(context.Background(), "c2"); c2.ListID != "l-todo" || len(fb.Comments["c2"]) != 0 {
		t.Fatal("unlabelled card was touched")
	}
	if s, _ := e.Paths.ReadStatus(); s.State != "idle" || !strings.Contains(s.Note, "ready-for-review") {
		t.Fatalf("status = %+v", s)
	}
}

func TestLiveNotReadyFallsBackToOnHold(t *testing.T) {
	noClarify := []spaceboard.List{allLists[0], allLists[1], allLists[2], allLists[4]}
	e, fb, pub, _ := setup(t, notReadyPre, noClarify, nil)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c, _ := fb.GetCard(context.Background(), "c1")
	if c.ListID != "l-hold" || pub.calls != 0 {
		t.Fatalf("card in %s pub=%d", c.ListID, pub.calls)
	}
	if !strings.Contains(fb.Comments["c1"][1], "NEEDS CLARIFICATION") {
		t.Fatalf("comments = %q", fb.Comments["c1"])
	}
	entries, _ := filepath.Glob(filepath.Join(e.Paths.RunsDir(), "BII-1", "*-chatgpt-prompt.md"))
	if len(entries) != 1 {
		t.Fatalf("chatgpt prompt file not written: %v", entries)
	}
}

func TestLiveNoChangesIsBlocked(t *testing.T) {
	e, fb, pub, _ := setup(t, readyPre, allLists, nil) // implementer changes nothing
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c, _ := fb.GetCard(context.Background(), "c1")
	if c.ListID != "l-hold" || pub.calls != 0 || !strings.Contains(fb.Comments["c1"][1], "AUTOPILOT BLOCKED") {
		t.Fatalf("card %s pub %d comments %q", c.ListID, pub.calls, fb.Comments["c1"])
	}
}

func TestKillSwitchStopsTick(t *testing.T) {
	e, fb, _, _ := setup(t, readyPre, allLists, nil)
	e.Paths.SetStop("test", e.now())
	if err := e.Tick(context.Background()); !errors.Is(err, runstate.ErrStopped) {
		t.Fatalf("err = %v", err)
	}
	if len(fb.Comments) != 0 {
		t.Fatal("stopped autopilot touched the board")
	}
}

func TestNoLabelNothingToDo(t *testing.T) {
	e, _, _, _ := setup(t, readyPre, allLists, nil)
	sp := e.Space.(fakeSpace)
	sp.info.Labels = nil
	e.Space = sp
	if err := e.Tick(context.Background()); !errors.Is(err, ErrNothingToDo) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigRefusesDoneRouting(t *testing.T) {
	c := Config{Boards: []BoardConfig{{BoardID: "b", RepoName: "r", RepoDir: "/x", ReviewList: "Done"}}}
	c.Defaults()
	if err := c.Validate(); err == nil {
		t.Fatal("a config routing cards into Done must be refused")
	}
}
