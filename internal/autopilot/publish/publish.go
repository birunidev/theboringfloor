// Package publish turns a passing card worktree into a reviewable change:
// commit, push the autopilot branch with an explicit refspec, and open (or
// reuse) a DRAFT pull request. It has no merge, no force push and no way to
// push anything but refs/heads/autopilot/*.
package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// BranchPrefix is the only namespace publish will push.
const BranchPrefix = "autopilot/"

// ErrUnsafePush: the push target is not an autopilot branch.
var ErrUnsafePush = errors.New("publish: refusing unsafe push")

// Target describes where a card's branch goes.
type Target struct {
	Dir    string // the card worktree
	Remote string // default "origin"
	Base   string // default "main"
	Branch string // autopilot/<KEY>
	Repo   string // owner/name for gh; derived from the remote URL when empty
}

func (t Target) remote() string {
	if t.Remote == "" {
		return "origin"
	}
	return t.Remote
}

func (t Target) base() string {
	if t.Base == "" {
		return "main"
	}
	return t.Base
}

// CheckTarget refuses anything but a well-formed autopilot branch.
func CheckTarget(t Target) error {
	b := t.Branch
	switch {
	case !strings.HasPrefix(b, BranchPrefix) || len(b) == len(BranchPrefix):
		return fmt.Errorf("%w: branch %q is outside %s*", ErrUnsafePush, b, BranchPrefix)
	case b == t.base() || b == "main" || b == "master":
		return fmt.Errorf("%w: branch %q is a base branch", ErrUnsafePush, b)
	case strings.ContainsAny(b, " :~^?*[\\") || strings.Contains(b, ".."):
		return fmt.Errorf("%w: branch %q", ErrUnsafePush, b)
	}
	return nil
}

// Commit stages everything in the worktree and commits it. Repository
// hooks run (no --no-verify). Returns the new HEAD, or the current HEAD
// with committed=false when there was nothing to commit.
func Commit(ctx context.Context, t Target, message string) (sha string, committed bool, err error) {
	if err := CheckTarget(t); err != nil {
		return "", false, err
	}
	if err := onBranch(ctx, t); err != nil {
		return "", false, err
	}
	if _, err := run(ctx, t.Dir, "git", "add", "-A"); err != nil {
		return "", false, err
	}
	if _, err := run(ctx, t.Dir, "git", "diff", "--cached", "--quiet"); err == nil {
		head, err := run(ctx, t.Dir, "git", "rev-parse", "HEAD")
		return head, false, err
	}
	if _, err := run(ctx, t.Dir, "git", "commit", "-q", "-m", message); err != nil {
		return "", false, err
	}
	head, err := run(ctx, t.Dir, "git", "rev-parse", "HEAD")
	return head, true, err
}

// Push pushes HEAD to refs/heads/<branch> on the remote. Never forced.
func Push(ctx context.Context, t Target) error {
	if err := CheckTarget(t); err != nil {
		return err
	}
	if err := onBranch(ctx, t); err != nil {
		return err
	}
	refspec := "HEAD:refs/heads/" + t.Branch
	_, err := run(ctx, t.Dir, "git", "push", "--quiet", t.remote(), refspec)
	return err
}

func onBranch(ctx context.Context, t Target) error {
	cur, err := run(ctx, t.Dir, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if cur != t.Branch {
		return fmt.Errorf("%w: worktree is on %q, not %q", ErrUnsafePush, cur, t.Branch)
	}
	return nil
}

var ghRepoRe = regexp.MustCompile(`github\.com[:/]+([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)

// RepoFromRemote derives owner/name from the remote's URL.
func RepoFromRemote(ctx context.Context, t Target) (string, error) {
	if t.Repo != "" {
		return t.Repo, nil
	}
	u, err := run(ctx, t.Dir, "git", "remote", "get-url", t.remote())
	if err != nil {
		return "", err
	}
	m := ghRepoRe.FindStringSubmatch(strings.TrimSpace(u))
	if m == nil {
		return "", fmt.Errorf("publish: remote %q is not a GitHub repository", u)
	}
	return m[1] + "/" + m[2], nil
}

// GhBin is the gh executable; THEFLOOR_GH_BIN overrides it (tests).
func GhBin() string {
	if v := os.Getenv("THEFLOOR_GH_BIN"); v != "" {
		return v
	}
	return "gh"
}

// DraftPR opens a draft pull request for the branch, or returns the URL of
// the open one that already exists for it.
func DraftPR(ctx context.Context, t Target, title, body string) (url string, created bool, err error) {
	if err := CheckTarget(t); err != nil {
		return "", false, err
	}
	repo, err := RepoFromRemote(ctx, t)
	if err != nil {
		return "", false, err
	}
	out, err := run(ctx, t.Dir, GhBin(), "pr", "list", "--repo", repo, "--head", t.Branch, "--state", "open", "--json", "url")
	if err != nil {
		return "", false, err
	}
	var existing []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(out), &existing) == nil && len(existing) > 0 && existing[0].URL != "" {
		return existing[0].URL, false, nil
	}
	f, err := os.CreateTemp("", "autopilot-pr-*.md")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return "", false, err
	}
	f.Close()
	out, err = run(ctx, t.Dir, GhBin(), "pr", "create", "--draft", "--repo", repo,
		"--base", t.base(), "--head", t.Branch, "--title", title, "--body-file", f.Name())
	if err != nil {
		return "", false, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), true, nil
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
