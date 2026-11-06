package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// isolate keeps the member's global git config and hooks out of the tests.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fixture: a bare "origin" with one commit on main, and a member clone that
// sits on its own feature branch with an uncommitted edit (like the real
// birunidev-space checkout).
func fixture(t *testing.T) (Repo, string) {
	t.Helper()
	isolate(t)
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	seed := filepath.Join(base, "seed")
	clone := filepath.Join(base, "clone")
	run(t, base, "init", "-q", "--bare", "-b", "main", origin)
	run(t, base, "init", "-q", "-b", "main", seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("hi\n"), 0o644)
	run(t, seed, "add", ".")
	run(t, seed, "commit", "-q", "-m", "init")
	run(t, seed, "remote", "add", "origin", origin)
	run(t, seed, "push", "-q", "origin", "main")
	run(t, base, "clone", "-q", origin, clone)
	run(t, clone, "checkout", "-q", "-b", "member-feature")
	os.WriteFile(filepath.Join(clone, "README.md"), []byte("member wip\n"), 0o644)
	return Repo{Name: "space", Dir: clone}, seed
}

func TestPrepareCreatesIsolatedWorktree(t *testing.T) {
	repo, seed := fixture(t)
	ctx := context.Background()
	m := Manager{Root: filepath.Join(t.TempDir(), "wt"), Fetch: true}

	// origin/main moves after the member last fetched: Prepare must fetch.
	os.WriteFile(filepath.Join(seed, "NEW.md"), []byte("new\n"), 0o644)
	run(t, seed, "add", ".")
	run(t, seed, "commit", "-q", "-m", "second")
	run(t, seed, "push", "-q", "origin", "main")
	want := run(t, seed, "rev-parse", "HEAD")

	wt, err := m.Prepare(ctx, repo, "BII-655")
	if err != nil {
		t.Fatal(err)
	}
	if wt.Branch != "autopilot/BII-655" || wt.Reused || wt.BaseSHA != want {
		t.Fatalf("wt = %+v, want base %s", wt, want)
	}
	if got := run(t, wt.Path, "rev-parse", "HEAD"); got != want {
		t.Fatalf("worktree HEAD %s, want %s", got, want)
	}
	// The member's checkout is untouched: same branch, same dirty file.
	if b := run(t, repo.Dir, "rev-parse", "--abbrev-ref", "HEAD"); b != "member-feature" {
		t.Fatalf("member branch changed to %s", b)
	}
	if body, _ := os.ReadFile(filepath.Join(repo.Dir, "README.md")); string(body) != "member wip\n" {
		t.Fatalf("member working file changed: %q", body)
	}
	// The new branch must not track origin/main (a push must never go there).
	if up, err := exec.Command("git", "-C", wt.Path, "rev-parse", "--abbrev-ref", "@{upstream}").CombinedOutput(); err == nil {
		t.Fatalf("branch tracks %s", up)
	}
}

func TestPrepareResumes(t *testing.T) {
	repo, _ := fixture(t)
	ctx := context.Background()
	m := Manager{Root: filepath.Join(t.TempDir(), "wt")}
	wt, err := m.Prepare(ctx, repo, "BII-1")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wt.Path, "work.txt"), []byte("x"), 0o644)
	again, err := m.Prepare(ctx, repo, "BII-1")
	if err != nil || !again.Reused || again.Path != wt.Path {
		t.Fatalf("resume: %+v %v", again, err)
	}
	if _, err := os.Stat(filepath.Join(again.Path, "work.txt")); err != nil {
		t.Fatal("resumed worktree lost its work")
	}
	// Worktree removed but branch kept: Prepare re-attaches the branch.
	run(t, wt.Path, "add", ".")
	run(t, wt.Path, "commit", "-q", "-m", "work")
	if err := m.Remove(ctx, repo, wt, false); err != nil {
		t.Fatal(err)
	}
	back, err := m.Prepare(ctx, repo, "BII-1")
	if err != nil || !back.Reused {
		t.Fatalf("reattach: %+v %v", back, err)
	}
	if _, err := os.Stat(filepath.Join(back.Path, "work.txt")); err != nil {
		t.Fatal("reattached branch lost its commit")
	}
}

func TestPrepareRefusesWrongBranchInPath(t *testing.T) {
	repo, _ := fixture(t)
	ctx := context.Background()
	m := Manager{Root: filepath.Join(t.TempDir(), "wt")}
	wt, err := m.Prepare(ctx, repo, "BII-2")
	if err != nil {
		t.Fatal(err)
	}
	run(t, wt.Path, "checkout", "-q", "-b", "something-else")
	if _, err := m.Prepare(ctx, repo, "BII-2"); err == nil {
		t.Fatal("a worktree on the wrong branch must not be reused")
	}
}

func TestChangedFilesAndDiff(t *testing.T) {
	repo, _ := fixture(t)
	ctx := context.Background()
	m := Manager{Root: filepath.Join(t.TempDir(), "wt")}
	wt, err := m.Prepare(ctx, repo, "BII-3")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wt.Path, "committed.go"), []byte("package x\n"), 0o644)
	run(t, wt.Path, "add", ".")
	run(t, wt.Path, "commit", "-q", "-m", "c")
	os.WriteFile(filepath.Join(wt.Path, "README.md"), []byte("edited\n"), 0o644)
	os.WriteFile(filepath.Join(wt.Path, "untracked.txt"), []byte("u"), 0o644)

	files, err := ChangedFiles(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"README.md", "committed.go", "untracked.txt"}; !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	d, err := Diff(ctx, wt)
	if err != nil || !strings.Contains(d, "+edited") || !strings.Contains(d, "committed.go") {
		t.Fatalf("diff missing changes: %v\n%s", err, d)
	}
}

func TestPrepareDetached(t *testing.T) {
	repo, _ := fixture(t)
	ctx := context.Background()
	m := Manager{Root: filepath.Join(t.TempDir(), "wt")}
	wt, err := m.PrepareDetached(ctx, repo, "BII-9")
	if err != nil {
		t.Fatal(err)
	}
	if b := run(t, wt.Path, "rev-parse", "--abbrev-ref", "HEAD"); b != "HEAD" {
		t.Fatalf("dry worktree is on branch %q, want detached", b)
	}
	if out := run(t, repo.Dir, "branch", "--list", "autopilot/*"); out != "" {
		t.Fatalf("dry run created a branch: %s", out)
	}
	if err := m.Remove(ctx, repo, wt, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatal("dry worktree not removed")
	}
}

func TestBranchSafety(t *testing.T) {
	for _, tc := range []struct {
		key, want string
	}{
		{"BII-655", "autopilot/BII-655"},
		{"../../main", "autopilot/main"},
		{"a b/c", "autopilot/a-b-c"},
		{"", "autopilot/card"},
	} {
		if got := BranchFor(tc.key); got != tc.want {
			t.Errorf("BranchFor(%q) = %q, want %q", tc.key, got, tc.want)
		}
		if err := CheckBranch(BranchFor(tc.key), "main"); err != nil {
			t.Errorf("CheckBranch(BranchFor(%q)): %v", tc.key, err)
		}
	}
	for _, bad := range []string{"main", "refs/heads/main", "master", "autopilot/", "feature/x", "autopilot/../main"} {
		if err := CheckBranch(bad, "main"); !errors.Is(err, ErrUnsafeBranch) {
			t.Errorf("CheckBranch(%q) = %v, want ErrUnsafeBranch", bad, err)
		}
	}
}
