// Package runstate holds the autopilot's local, durable state: where its
// files live, the kill switch, token budgets, the single-instance lock, the
// per-run JSONL logs, and the live status file the TUI cockpit reads.
//
// Everything lives under one root (default ~/.theboringfloor/autopilot).
// The root is under $HOME on purpose: opencode only loads the
// birunidev-space MCP from ~/opencode.json for directories under $HOME.
package runstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// Paths is the autopilot's file layout under one root.
type Paths struct{ Root string }

// DefaultRoot returns $THEFLOOR_AUTOPILOT_HOME, else ~/.theboringfloor/autopilot.
func DefaultRoot() string {
	if v := os.Getenv("THEFLOOR_AUTOPILOT_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".theboringfloor", "autopilot")
}

func (p Paths) StopFile() string     { return filepath.Join(p.Root, "STOP") }
func (p Paths) LockFile() string     { return filepath.Join(p.Root, "autopilot.lock") }
func (p Paths) UsageFile() string    { return filepath.Join(p.Root, "usage.json") }
func (p Paths) StatusFile() string   { return filepath.Join(p.Root, "status.json") }
func (p Paths) RunsDir() string      { return filepath.Join(p.Root, "runs") }
func (p Paths) WorktreesDir() string { return filepath.Join(p.Root, "worktrees") }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SafeName makes a card key or run id usable as one path element.
func SafeName(s string) string {
	s = unsafeName.ReplaceAllString(s, "_")
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

// Ensure creates the root and its subdirectories, private to the user.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Root, p.RunsDir(), p.WorktreesDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// writeJSONAtomic writes v to path via a temp file + rename, mode 0600.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
