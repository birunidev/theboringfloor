// Package projects validates and registers board/repository mappings without
// starting an agent or writing to a board.
package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/engine"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

type Document struct {
	Config engine.Config
	Raw    []byte
	Exists bool
}

func Load(path string) (Document, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := engine.Config{}
		c.Defaults()
		return Document{Config: c}, nil
	}
	if err != nil {
		return Document{}, err
	}
	var c engine.Config
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Document{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Document{}, errors.New("configuration must contain exactly one JSON object")
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		return Document{}, err
	}
	return Document{Config: c, Raw: b, Exists: true}, nil
}

// Select also recognizes the existing repo_name entries, so previously
// configured projects need no migration or additional registry.
func Select(c engine.Config, ref string) (engine.BoardConfig, error) {
	var matches []engine.BoardConfig
	for _, b := range c.Boards {
		if strings.EqualFold(ref, b.Name) || strings.EqualFold(ref, b.RepoName) || ref == b.BoardID {
			matches = append(matches, b)
		}
	}
	if len(matches) == 0 {
		return engine.BoardConfig{}, fmt.Errorf("project %q is not registered; use 'project list' or 'project add'", ref)
	}
	if len(matches) != 1 {
		return engine.BoardConfig{}, fmt.Errorf("project %q is ambiguous; use its exact board ID", ref)
	}
	return matches[0], nil
}

func ValidateMapping(b engine.BoardConfig) error {
	if !validName.MatchString(b.Name) || strings.EqualFold(b.Name, "all") {
		return errors.New("project name must use letters, digits, underscores or hyphens; 'all' is reserved")
	}
	if !validName.MatchString(b.RepoName) {
		return errors.New("invalid repository worktree name")
	}
	if b.Base == "" {
		return errors.New("--base is required; choose the branch containing the project code")
	}
	if b.RepoDir == "" || b.BoardID == "" {
		return errors.New("--repo and --board are required")
	}
	if len(b.Checks) == 0 {
		return errors.New("at least one --check JSON argv array is required; no verification commands are guessed")
	}
	for _, ch := range b.Checks {
		if len(ch.Argv) == 0 || strings.TrimSpace(ch.Argv[0]) == "" {
			return errors.New("check must have a nonempty executable")
		}
	}
	c := engine.Config{Boards: []engine.BoardConfig{b}}
	return c.Validate()
}

func ValidateChecks(b engine.BoardConfig) error {
	if len(b.Checks) == 0 {
		return fmt.Errorf("project %q has no sandboxed checks configured", b.Name)
	}
	for _, check := range b.Checks {
		if len(check.Argv) == 0 || strings.TrimSpace(check.Argv[0]) == "" {
			return fmt.Errorf("project %q has an empty check", b.Name)
		}
	}
	return nil
}

func ValidateRepo(ctx context.Context, dir, base string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("repository path: %w", err)
	}
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", abs}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("--repo must be an existing Git checkout")
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil || top != abs {
		return "", errors.New("--repo must point to the Git repository root")
	}
	if _, err := git("check-ref-format", "--branch", base); err != nil {
		return "", errors.New("invalid base branch")
	}
	if _, err := git("remote", "get-url", "origin"); err != nil {
		return "", errors.New("repository needs an origin remote; publishing requires GitHub access through gh")
	}
	if _, err := git("show-ref", "--verify", "--quiet", "refs/remotes/origin/"+base); err != nil {
		return "", errors.New("base is not available locally as origin/<base>; fetch the intended base branch first")
	}
	return abs, nil
}

func ValidateReadOnly(paths []string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	home, _ = filepath.EvalSymlinks(home)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		abs, err = filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("read-only dependency path: %w", err)
		}
		if abs == "/" || abs == home {
			return nil, errors.New("cannot expose the filesystem root or entire home to checks")
		}
		out = append(out, abs)
	}
	return out, nil
}

func ValidateBoard(ctx context.Context, api engine.SpaceAPI, b engine.BoardConfig) (engine.Resolved, error) {
	info, err := api.GetBoard(ctx, b.BoardID)
	if err != nil {
		return engine.Resolved{}, err
	}
	if info.ID != b.BoardID {
		return engine.Resolved{}, errors.New("board response does not match the requested board ID")
	}
	if strings.TrimSpace(info.Description) == "" && len(b.PolicyCards) == 0 {
		return engine.Resolved{}, errors.New("board needs a policy description or --policy card before it can be registered")
	}
	// Resolve uses names; ambiguous names must not silently select a list.
	for _, name := range []string{b.ReadyList, b.ReviewList, b.BlockedList, b.ClarifyList, b.DoneList, b.WorkingList} {
		if name == "" {
			continue
		}
		count := 0
		for _, list := range info.Lists {
			if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(list.Name)) {
				count++
			}
		}
		if count > 1 {
			return engine.Resolved{}, fmt.Errorf("board has multiple lists named %q", name)
		}
	}
	r, err := (&engine.Engine{Space: &cachedBoard{SpaceAPI: api, info: info}}).Resolve(ctx, b)
	if err != nil {
		return r, err
	}
	for _, id := range []string{r.Ready, r.Working, r.Review, r.Blocked, r.Clarify} {
		if id != "" && id == r.Done {
			return r, errors.New("board list IDs route work into Done")
		}
	}
	return r, nil
}

type cachedBoard struct {
	engine.SpaceAPI
	info spaceboard.BoardInfo
}

func (c *cachedBoard) GetBoard(context.Context, string) (spaceboard.BoardInfo, error) {
	return c.info, nil
}

func Append(c engine.Config, b engine.BoardConfig) (engine.Config, error) {
	for _, existing := range c.Boards {
		if existing.BoardID == b.BoardID {
			return c, errors.New("board is already registered; use its existing project")
		}
		if strings.EqualFold(existing.Name, b.Name) || strings.EqualFold(existing.RepoName, b.Name) || strings.EqualFold(existing.Name, b.RepoName) || strings.EqualFold(existing.RepoName, b.RepoName) {
			return c, errors.New("project name conflicts with an existing name or repository")
		}
		existingDir, _ := filepath.EvalSymlinks(existing.RepoDir)
		if existing.RepoDir == b.RepoDir || existingDir == b.RepoDir {
			return c, errors.New("repository is already registered; use its existing project")
		}
	}
	c.Boards = append(append([]engine.BoardConfig(nil), c.Boards...), b)
	return c, c.Validate()
}

// Save refuses live-worker changes and concurrent config edits, then replaces
// only the config atomically. It never touches STOP, status, usage or boards.
func Save(path, lockPath string, before Document, next engine.Config) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return err
	}
	lock, err := runstate.AcquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("exit the running worker before registering a project: %w", err)
	}
	defer lock.Release()
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if before.Exists != (err == nil) || !bytes.Equal(current, before.Raw) {
		return errors.New("configuration changed during validation; retry without overwriting the newer file")
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".project-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
