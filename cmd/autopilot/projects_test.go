package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/engine"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/projects"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard"
)

type projectSpace struct {
	*board.Fake
	info spaceboard.BoardInfo
}

func (p *projectSpace) GetBoard(context.Context, string) (spaceboard.BoardInfo, error) {
	return p.info, nil
}

func projectFixture(t *testing.T) (runstate.Paths, string, *projectSpace) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	git("remote", "add", "origin", "https://github.com/example/project.git")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	api := &projectSpace{Fake: board.NewFake(), info: spaceboard.BoardInfo{ID: "b1", Name: "Clear board", Description: "Follow explicit founder approval on each scoped card.", Lists: []spaceboard.List{{ID: "queue", Name: "Autopilot Queue"}, {ID: "review", Name: "Review"}, {ID: "hold", Name: "On Hold"}, {ID: "done", Name: "Done"}}}}
	return runstate.Paths{Root: t.TempDir()}, repo, api
}

func TestProjectAddPreviewSaveAndRunPlans(t *testing.T) {
	paths, repo, api := projectFixture(t)
	ctx := context.Background()
	var out bytes.Buffer
	call := func(args ...string) ([]string, error) {
		out.Reset()
		return projectCommand(ctx, paths, args, &out, func() engine.SpaceAPI { return api })
	}
	args := []string{"add", "example", "--repo", repo, "--board", "b1", "--base", "main", "--check", `["must-not-execute","argument with spaces"]`}
	if plan, err := call(append(append([]string{}, args...), "--preview")...); err != nil || plan != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Root, "config.json")); !os.IsNotExist(err) {
		t.Fatal("preview wrote config")
	}
	if plan, err := call(args...); err != nil || plan != nil {
		t.Fatalf("add: %v", err)
	}
	doc, err := projects.Load(filepath.Join(paths.Root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Config.Boards) != 1 || !reflect.DeepEqual(doc.Config.Boards[0].Checks[0].Argv, []string{"must-not-execute", "argument with spaces"}) {
		t.Fatal("argv was shell-split or lost")
	}
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"run", "example"}, []string{"--config", filepath.Join(paths.Root, "config.json"), "--board", "b1", "--dry-run"}},
		{[]string{"run", "example", "--live", "--once"}, []string{"--config", filepath.Join(paths.Root, "config.json"), "--board", "b1", "--live", "--once"}},
		{[]string{"run", "all", "--live"}, []string{"--config", filepath.Join(paths.Root, "config.json"), "--board", "b1", "--live"}},
		{[]string{"run", "example", "--card", "BII-1"}, []string{"--config", filepath.Join(paths.Root, "config.json"), "--board", "b1", "--dry-run", "--card", "BII-1"}},
	} {
		plan, err := call(tc.args...)
		if err != nil || !reflect.DeepEqual(plan, tc.want) {
			t.Fatalf("plan %v: %v %v", tc.args, plan, err)
		}
	}
	if _, err := call("list"); err != nil || !strings.Contains(out.String(), "example") {
		t.Fatal("list failed")
	}
	if _, err := call(args...); err == nil {
		t.Fatal("duplicate board registered")
	}
	if len(api.Moves) != 0 || len(api.Comments) != 0 {
		t.Fatal("registration/selection wrote to board")
	}
}

func TestProjectRegistrationCannotBypassWorkerLock(t *testing.T) {
	paths, repo, api := projectFixture(t)
	lock, err := runstate.AcquireLock(paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	var out bytes.Buffer
	_, err = projectCommand(context.Background(), paths, []string{"add", "example", "--repo", repo, "--base", "main", "--board", "b1", "--check", `["true"]`}, &out, func() engine.SpaceAPI { return api })
	if !errors.Is(err, runstate.ErrLocked) {
		t.Fatalf("lock bypassed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Root, "config.json")); !os.IsNotExist(err) {
		t.Fatal("active configuration overwritten")
	}
}

func TestProjectCheckIsReadOnlyAndHonorsExistingPolicy(t *testing.T) {
	paths, repo, api := projectFixture(t)
	c := engine.Config{Boards: []engine.BoardConfig{{Name: "Existing board", RepoName: "repo", RepoDir: repo, BoardID: "b1", Base: "main", ReadyList: "Autopilot Queue", Label: "none", Checks: []engine.Check{{Name: "tests", Argv: []string{"must-not-execute"}}}}}}
	c.Defaults()
	data, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(paths.Root, "config.json"), data, 0600)
	bin := t.TempDir()
	for _, tool := range []string{"opencode", "bwrap", "gh"} {
		os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\nexit 99\n"), 0700)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out bytes.Buffer
	plan, err := projectCommand(context.Background(), paths, []string{"check", "repo"}, &out, func() engine.SpaceAPI { return api })
	if err != nil || plan != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out.String(), "founder holds are still checked") {
		t.Fatal("metadata check claimed authorization")
	}
	got, _ := os.ReadFile(filepath.Join(paths.Root, "config.json"))
	if !bytes.Equal(data, got) || len(api.Moves) != 0 || len(api.Comments) != 0 {
		t.Fatal("check mutated data")
	}
}

func TestProjectRunAllKeepsAllConfiguredBoards(t *testing.T) {
	paths, repo, _ := projectFixture(t)
	c := engine.Config{}
	for _, id := range []string{"b1", "b2"} {
		c.Boards = append(c.Boards, engine.BoardConfig{Name: id, RepoName: id, RepoDir: repo, BoardID: id, Base: "main", ReadyList: "Autopilot Queue", Label: "none", Checks: []engine.Check{{Name: "test", Argv: []string{"true"}}}})
	}
	c.Defaults()
	data, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(paths.Root, "config.json"), data, 0600)
	var out bytes.Buffer
	plan, err := projectCommand(context.Background(), paths, []string{"run", "all", "--live"}, &out, func() engine.SpaceAPI { t.Fatal("run planning queried boards"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range plan {
		if arg == "--board" {
			t.Fatal("all projects filtered to one")
		}
	}
}

func TestProjectInvalidInputsDoNotConnect(t *testing.T) {
	paths := runstate.Paths{Root: t.TempDir()}
	for _, args := range [][]string{{"help"}, {"add", "--help"}, {"run", "unknown", "--once"}, {"run", "unknown", "--live", "--card", "BII-1"}, {"run", "all", "--card", "BII-1"}, {"add", "bad", "--check", "not-json"}, {"unknown"}} {
		var out bytes.Buffer
		_, err := projectCommand(context.Background(), paths, args, &out, func() engine.SpaceAPI { t.Fatal("invalid input connected to board"); return nil })
		if args[0] == "help" || (len(args) > 1 && args[1] == "--help") {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}
