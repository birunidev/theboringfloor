package publish

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setup(t *testing.T) (Target, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "t")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@example.com")
	}
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	work := filepath.Join(base, "work")
	git(t, base, "init", "-q", "--bare", "-b", "main", origin)
	git(t, base, "init", "-q", "-b", "main", work)
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644)
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "init")
	git(t, work, "remote", "add", "origin", origin)
	git(t, work, "push", "-q", "origin", "main")
	git(t, work, "checkout", "-q", "-b", "autopilot/BII-1")
	return Target{Dir: work, Branch: "autopilot/BII-1", Repo: "birunidev/birunidev-space"}, origin
}

func TestCommitAndPushOnlyAutopilotBranch(t *testing.T) {
	tg, origin := setup(t)
	ctx := context.Background()
	mainBefore := git(t, origin, "rev-parse", "refs/heads/main")

	if _, committed, err := Commit(ctx, tg, "nothing"); err != nil || committed {
		t.Fatalf("empty commit: committed=%v err=%v", committed, err)
	}
	os.WriteFile(filepath.Join(tg.Dir, "b.txt"), []byte("b\n"), 0o644)
	sha, committed, err := Commit(ctx, tg, "feat: b")
	if err != nil || !committed || sha == "" {
		t.Fatalf("commit: %s %v %v", sha, committed, err)
	}
	if err := Push(ctx, tg); err != nil {
		t.Fatal(err)
	}
	if got := git(t, origin, "rev-parse", "refs/heads/autopilot/BII-1"); got != sha {
		t.Fatalf("remote branch at %s, want %s", got, sha)
	}
	if got := git(t, origin, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatal("main moved on the remote")
	}
}

func TestPushRefusals(t *testing.T) {
	tg, origin := setup(t)
	ctx := context.Background()
	mainBefore := git(t, origin, "rev-parse", "refs/heads/main")
	for _, b := range []string{"main", "master", "autopilot/", "feature/x", "autopilot/x:main", "autopilot/../main", "autopilot/a b"} {
		bad := tg
		bad.Branch = b
		if err := Push(ctx, bad); !errors.Is(err, ErrUnsafePush) {
			t.Errorf("Push(%q) = %v, want ErrUnsafePush", b, err)
		}
		if _, _, err := Commit(ctx, bad, "x"); !errors.Is(err, ErrUnsafePush) {
			t.Errorf("Commit(%q) = %v, want ErrUnsafePush", b, err)
		}
	}
	// Worktree on another branch than the target: refuse.
	git(t, tg.Dir, "checkout", "-q", "main")
	if err := Push(ctx, tg); !errors.Is(err, ErrUnsafePush) {
		t.Fatalf("push from main checkout = %v", err)
	}
	if got := git(t, origin, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatal("main moved on the remote")
	}
}

func TestPushNeverForces(t *testing.T) {
	tg, origin := setup(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(tg.Dir, "b.txt"), []byte("b\n"), 0o644)
	Commit(ctx, tg, "one")
	if err := Push(ctx, tg); err != nil {
		t.Fatal(err)
	}
	remoteSHA := git(t, origin, "rev-parse", "refs/heads/autopilot/BII-1")
	git(t, tg.Dir, "reset", "-q", "--hard", "HEAD~1")
	os.WriteFile(filepath.Join(tg.Dir, "c.txt"), []byte("c\n"), 0o644)
	Commit(ctx, tg, "rewritten")
	if err := Push(ctx, tg); err == nil {
		t.Fatal("a non-fast-forward push must fail, not force")
	}
	if got := git(t, origin, "rev-parse", "refs/heads/autopilot/BII-1"); got != remoteSHA {
		t.Fatal("remote branch was overwritten")
	}
}

func TestRepoFromRemote(t *testing.T) {
	tg, _ := setup(t)
	ctx := context.Background()
	for url, want := range map[string]string{
		"git@github.com:birunidev/birunidev-space.git":    "birunidev/birunidev-space",
		"https://github.com/birunidev/birunidev-lab":      "birunidev/birunidev-lab",
		"https://github.com/birunidev/birunidev-lab.git/": "birunidev/birunidev-lab",
	} {
		git(t, tg.Dir, "remote", "set-url", "origin", url)
		got, err := RepoFromRemote(ctx, Target{Dir: tg.Dir})
		if err != nil || got != want {
			t.Errorf("%s -> %q %v, want %q", url, got, err, want)
		}
	}
	git(t, tg.Dir, "remote", "set-url", "origin", "https://gitlab.com/x/y.git")
	if _, err := RepoFromRemote(ctx, Target{Dir: tg.Dir}); err == nil {
		t.Fatal("non-GitHub remote accepted")
	}
}

// fakeGh records its arguments and simulates `pr list` / `pr create`.
func fakeGh(t *testing.T, existing string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "gh")
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> " + log + "\n" +
		"case \"$2\" in\n" +
		"  list) echo '" + existing + "' ;;\n" +
		"  create) echo 'https://github.com/birunidev/birunidev-space/pull/42' ;;\n" +
		"  *) exit 9 ;;\n" +
		"esac\n"
	os.WriteFile(bin, []byte(script), 0o755)
	return bin, log
}

func TestDraftPR(t *testing.T) {
	tg, _ := setup(t)
	ctx := context.Background()
	bin, log := fakeGh(t, "[]")
	t.Setenv("THEFLOOR_GH_BIN", bin)
	url, created, err := DraftPR(ctx, tg, "BII-1: thing", "body")
	if err != nil || !created || url != "https://github.com/birunidev/birunidev-space/pull/42" {
		t.Fatalf("%s %v %v", url, created, err)
	}
	calls, _ := os.ReadFile(log)
	c := string(calls)
	for _, want := range []string{"pr create --draft", "--repo birunidev/birunidev-space", "--base main", "--head autopilot/BII-1"} {
		if !strings.Contains(c, want) {
			t.Errorf("gh call missing %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "merge") {
		t.Fatal("gh was asked to merge")
	}

	bin2, log2 := fakeGh(t, `[{"url":"https://github.com/birunidev/birunidev-space/pull/7"}]`)
	t.Setenv("THEFLOOR_GH_BIN", bin2)
	url, created, err = DraftPR(ctx, tg, "t", "b")
	if err != nil || created || !strings.HasSuffix(url, "/pull/7") {
		t.Fatalf("reuse: %s %v %v", url, created, err)
	}
	if calls, _ := os.ReadFile(log2); strings.Contains(string(calls), "create") {
		t.Fatal("created a second PR for the same branch")
	}
}
