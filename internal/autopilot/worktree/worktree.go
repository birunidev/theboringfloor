// Package worktree gives every card its own git worktree and branch, cut
// from the remote's base branch, so the autopilot never edits a member's
// own checkout and never commits on the base branch.
//
//	<root>/<repo>/<KEY>   worktree on branch autopilot/<KEY>, from origin/<base>
//
// The member's checkout (Repo.Dir) is only used as the object store: `git
// worktree add` does not change which branch it has checked out or touch its
// working files.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// BranchPrefix is the only branch namespace the autopilot may create or use.
const BranchPrefix = "autopilot/"

// ErrUnsafeBranch: a branch outside autopilot/* or equal to the base.
var ErrUnsafeBranch = errors.New("worktree: unsafe branch")

// Repo is one source repository the autopilot works in.
type Repo struct {
	Name   string // short name, used as a directory, e.g. "birunidev-space"
	Dir    string // the member's existing clone
	Remote string // default "origin"
	Base   string // default "main"
}

func (r Repo) remote() string {
	if r.Remote == "" {
		return "origin"
	}
	return r.Remote
}

func (r Repo) base() string {
	if r.Base == "" {
		return "main"
	}
	return r.Base
}

// BaseRef is the remote-tracking ref the worktree is cut from.
func (r Repo) BaseRef() string { return r.remote() + "/" + r.base() }

// Worktree is one card's checkout.
type Worktree struct {
	Path    string
	Branch  string
	BaseSHA string // origin/<base> when the worktree was prepared
	Reused  bool   // an existing worktree for this card was resumed
}

// Manager prepares and removes card worktrees under Root.
type Manager struct {
	Root string
	// Fetch controls whether Prepare fetches the base branch first.
	Fetch bool
}

var unsafeKey = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// BranchFor returns the autopilot branch for a card key.
func BranchFor(cardKey string) string {
	k := strings.Trim(unsafeKey.ReplaceAllString(cardKey, "-"), "-.")
	if k == "" {
		k = "card"
	}
	return BranchPrefix + k
}

// CheckBranch refuses any branch the autopilot must never work on.
func CheckBranch(branch, base string) error {
	b := strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	switch {
	case !strings.HasPrefix(b, BranchPrefix) || len(b) == len(BranchPrefix):
		return fmt.Errorf("%w: %q is outside %s*", ErrUnsafeBranch, branch, BranchPrefix)
	case b == base || b == "main" || b == "master":
		return fmt.Errorf("%w: %q is a base branch", ErrUnsafeBranch, branch)
	case strings.Contains(b, ".."):
		return fmt.Errorf("%w: %q", ErrUnsafeBranch, branch)
	}
	return nil
}

// Prepare returns the card's worktree, creating it (and its branch from
// origin/<base>) if needed, or resuming the existing one.
func (m Manager) Prepare(ctx context.Context, repo Repo, cardKey string) (Worktree, error) {
	if repo.Name == "" || repo.Dir == "" {
		return Worktree{}, errors.New("worktree: repo name and dir are required")
	}
	branch := BranchFor(cardKey)
	if err := CheckBranch(branch, repo.base()); err != nil {
		return Worktree{}, err
	}
	if m.Fetch {
		if _, err := git(ctx, repo.Dir, "fetch", "--quiet", repo.remote(), repo.base()); err != nil {
			return Worktree{}, err
		}
	}
	baseSHA, err := git(ctx, repo.Dir, "rev-parse", "--verify", repo.BaseRef()+"^{commit}")
	if err != nil {
		return Worktree{}, fmt.Errorf("worktree: base %s not found: %w", repo.BaseRef(), err)
	}
	path := filepath.Join(m.Root, unsafeKey.ReplaceAllString(repo.Name, "_"), strings.TrimPrefix(branch, BranchPrefix))
	wt := Worktree{Path: path, Branch: branch, BaseSHA: baseSHA}

	if _, err := os.Stat(path); err == nil {
		cur, err := git(ctx, path, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return Worktree{}, fmt.Errorf("worktree: %s exists but is not a git worktree: %w", path, err)
		}
		if cur != branch {
			return Worktree{}, fmt.Errorf("worktree: %s is on %q, want %q", path, cur, branch)
		}
		wt.Reused = true
		return wt, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Worktree{}, err
	}
	if _, err := git(ctx, repo.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		// The branch survives from an earlier run (its worktree was removed).
		_, err = git(ctx, repo.Dir, "worktree", "add", "--quiet", path, branch)
		if err != nil {
			return Worktree{}, err
		}
		wt.Reused = true
		return wt, nil
	}
	if _, err := git(ctx, repo.Dir, "worktree", "add", "--quiet", "--no-track", "-b", branch, path, repo.BaseRef()); err != nil {
		return Worktree{}, err
	}
	return wt, nil
}

// PrepareDetached creates a throwaway worktree at origin/<base> with no
// branch, for read-only work (dry runs). Remove it with RemoveDetached.
func (m Manager) PrepareDetached(ctx context.Context, repo Repo, name string) (Worktree, error) {
	if repo.Name == "" || repo.Dir == "" {
		return Worktree{}, errors.New("worktree: repo name and dir are required")
	}
	if m.Fetch {
		if _, err := git(ctx, repo.Dir, "fetch", "--quiet", repo.remote(), repo.base()); err != nil {
			return Worktree{}, err
		}
	}
	baseSHA, err := git(ctx, repo.Dir, "rev-parse", "--verify", repo.BaseRef()+"^{commit}")
	if err != nil {
		return Worktree{}, fmt.Errorf("worktree: base %s not found: %w", repo.BaseRef(), err)
	}
	path := filepath.Join(m.Root, unsafeKey.ReplaceAllString(repo.Name, "_"), "dry-"+strings.Trim(unsafeKey.ReplaceAllString(name, "-"), "-."))
	_ = os.RemoveAll(path)
	_, _ = git(ctx, repo.Dir, "worktree", "prune")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Worktree{}, err
	}
	if _, err := git(ctx, repo.Dir, "worktree", "add", "--quiet", "--detach", path, baseSHA); err != nil {
		return Worktree{}, err
	}
	return Worktree{Path: path, BaseSHA: baseSHA}, nil
}

// Remove deletes the worktree directory. The branch is kept: it backs the PR.
func (m Manager) Remove(ctx context.Context, repo Repo, wt Worktree, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := git(ctx, repo.Dir, append(args, wt.Path)...)
	return err
}

// ChangedFiles lists every path the card changed relative to its base:
// committed on the branch, staged, unstaged, and untracked. Sorted, unique.
func ChangedFiles(ctx context.Context, wt Worktree) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			if line = strings.TrimSpace(line); line != "" && !seen[line] {
				seen[line] = true
				out = append(out, line)
			}
		}
	}
	committed, err := git(ctx, wt.Path, "diff", "--name-only", wt.BaseSHA+"...HEAD")
	if err != nil {
		return nil, err
	}
	add(committed)
	local, err := git(ctx, wt.Path, "diff", "--name-only", "HEAD")
	if err != nil {
		return nil, err
	}
	add(local)
	untracked, err := git(ctx, wt.Path, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	add(untracked)
	sort.Strings(out)
	return out, nil
}

// markUntracked records untracked files as intent-to-add so `git diff`
// includes new files. It stages no content; the commit still decides.
func markUntracked(ctx context.Context, wt Worktree) error {
	untracked, err := git(ctx, wt.Path, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	var files []string
	for _, f := range strings.Split(untracked, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil
	}
	_, err = git(ctx, wt.Path, append([]string{"add", "--intent-to-add", "--"}, files...)...)
	return err
}

// Diff is the full patch of the card's changes against its base: committed,
// staged, unstaged and new (untracked) files.
func Diff(ctx context.Context, wt Worktree) (string, error) {
	if err := markUntracked(ctx, wt); err != nil {
		return "", err
	}
	return git(ctx, wt.Path, "diff", wt.BaseSHA)
}

// DiffCheck runs `git diff --check` against the base (whitespace errors and
// leftover conflict markers), including new files. ok is false when git
// reports problems; output holds its report.
func DiffCheck(ctx context.Context, wt Worktree) (output string, ok bool, err error) {
	if err := markUntracked(ctx, wt); err != nil {
		return "", false, err
	}
	cmd := exec.CommandContext(ctx, "git", "diff", "--check", wt.BaseSHA)
	cmd.Dir = wt.Path
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	var ee *exec.ExitError
	switch {
	case runErr == nil:
		return "", true, nil
	case errors.As(runErr, &ee) && ee.ExitCode() == 2:
		return strings.TrimSpace(out.String()), false, nil
	default:
		return strings.TrimSpace(out.String()), false, fmt.Errorf("git diff --check: %w", runErr)
	}
}

// HeadSHA returns the worktree's current HEAD commit.
func HeadSHA(ctx context.Context, wt Worktree) (string, error) {
	return git(ctx, wt.Path, "rev-parse", "HEAD")
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
