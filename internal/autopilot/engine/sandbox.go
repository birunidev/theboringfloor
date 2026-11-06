package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Sandbox wraps check commands in bubblewrap. Checks execute code the
// implementer agent wrote, so they run with:
//   - the whole filesystem read-only,
//   - $HOME replaced by an empty tmpfs (no ~/.ssh, tokens, configs),
//   - only the worktree writable, plus explicitly listed read-only paths,
//   - a private /tmp, no network, and death with the parent.
type Sandbox struct {
	Worktree string
	ReadOnly []string // extra paths under $HOME to expose read-only (venv, deps)
	Bwrap    string   // default: "bwrap" on PATH
}

// ErrNoSandbox: bubblewrap is unavailable; checks refuse to run unsandboxed.
var ErrNoSandbox = errors.New("sandbox: bubblewrap (bwrap) not found; checks will not run unsandboxed")

// Wrap returns the argv that runs argv inside the sandbox.
func (s Sandbox) Wrap(argv []string) ([]string, error) {
	bw := s.Bwrap
	if bw == "" {
		p, err := exec.LookPath("bwrap")
		if err != nil {
			return nil, ErrNoSandbox
		}
		bw = p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return nil, fmt.Errorf("sandbox: cannot determine a safe home directory")
	}
	wt, err := filepath.Abs(s.Worktree)
	if err != nil {
		return nil, err
	}
	out := []string{bw,
		"--die-with-parent", "--new-session", "--unshare-net", "--unshare-pid", "--unshare-ipc",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--tmpfs", home,
	}
	for _, p := range s.ReadOnly {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		if abs == home || abs == "/" {
			return nil, fmt.Errorf("sandbox: refusing to expose %q", abs)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("sandbox: read-only path %q: %w", abs, err)
		}
		out = append(out, "--ro-bind", abs, abs)
	}
	out = append(out, "--bind", wt, wt, "--chdir", wt, "--setenv", "HOME", home, "--setenv", "CI", "1", "--")
	return append(out, argv...), nil
}
