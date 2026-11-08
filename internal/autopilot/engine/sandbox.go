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
//   - a private /tmp, no network, a cleared environment, and death with the parent.
type Sandbox struct {
	Worktree string
	ReadOnly []string // extra paths under $HOME to expose read-only (venv, deps)
	Bwrap    string   // default: "bwrap" on PATH
	// Network shares the host network namespace for one dependency-install
	// check. bubblewrap cannot filter hosts, so the install is kept narrow:
	// the validated command is `npm ci --ignore-scripts`, HOME is an empty
	// tmpfs (no ~/.npmrc or tokens), the environment is cleared, and the
	// registry is pinned by npm_config_registry below.
	Network bool
}

// AllowedNPMRegistry is the only registry a networked install is pointed at.
const AllowedNPMRegistry = "https://registry.npmjs.org/"

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
		"--die-with-parent", "--new-session", "--unshare-pid", "--unshare-ipc"}
	if !s.Network {
		out = append(out, "--unshare-net")
	}
	out = append(out,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--tmpfs", home,
	)
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
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	// --clearenv: tokens or keys in the member's environment never reach
	// agent-written code or its output.
	out = append(out, "--bind", wt, wt, "--chdir", wt, "--clearenv",
		"--setenv", "PATH", path, "--setenv", "HOME", home, "--setenv", "LANG", "C.UTF-8",
		"--setenv", "TMPDIR", "/tmp", "--setenv", "CI", "1")
	if s.Network {
		out = append(out,
			"--setenv", "npm_config_registry", AllowedNPMRegistry,
			"--setenv", "npm_config_ignore_scripts", "true",
			"--setenv", "npm_config_cache", "/tmp/npm-cache",
			"--setenv", "npm_config_audit", "false",
			"--setenv", "npm_config_fund", "false",
			"--setenv", "npm_config_update_notifier", "false")
	}
	out = append(out, "--")
	return append(out, argv...), nil
}
