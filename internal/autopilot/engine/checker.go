package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/worktree"
)

// CmdChecker runs configured checks directly (no shell) in the worktree.
// Checks run agent-written code, so they require a Sandbox unless
// Unsandboxed is set (only for trusted setup commands and tests).
type CmdChecker struct {
	Dir         string
	Checks      []Check
	Timeout     time.Duration // per command, default 15m
	Sandbox     *Sandbox
	Unsandboxed bool
	// Require runs before any check (profile preconditions). Its error is a
	// failed check, never an infrastructure error and never a silent pass.
	Require func(dir string) error
}

// minimalEnv is all a check gets from the member's environment. HOME is the
// worktree, never the member's real home, so even a command that escaped the
// sandbox would not find ~/.ssh or ~/.config by following $HOME. This is
// environment hygiene only; the filesystem boundary is the sandbox.
func minimalEnv(home string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	return []string{"PATH=" + path, "HOME=" + home, "LANG=C.UTF-8", "CI=1"}
}

// zeroTestMarkers are runner outputs that mean "nothing was tested", which
// never counts as a pass even when the runner exits 0.
var zeroTestMarkers = regexp.MustCompile(`(?mi)(^collected 0 items|\bno tests ran\b|no test files found|^# tests 0\s*$|\bno tests found\b|0 tests? collected|^\d+ skipped in [\d.]+s|^\s*Tests\s+\d+ skipped \(\d+\)\s*$|all tests were skipped)`)

// Run implements pipeline.Checker. A command that cannot start counts as a
// failed check, not an infrastructure error.
func (c CmdChecker) Run(ctx context.Context) ([]pipeline.CheckResult, error) {
	to := c.Timeout
	if to <= 0 {
		to = 15 * time.Minute
	}
	var out []pipeline.CheckResult
	if c.Require != nil {
		if err := c.Require(c.Dir); err != nil {
			return []pipeline.CheckResult{{Name: "profile preconditions", Cmd: "verify profile requirements", ExitCode: -1, Passed: false, Output: err.Error()}}, nil
		}
	}
	for _, ch := range c.Checks {
		if len(ch.Argv) == 0 {
			continue
		}
		argv := ch.Argv
		if !c.Unsandboxed {
			if c.Sandbox == nil {
				return out, ErrNoSandbox
			}
			if ch.Network {
				if err := PreflightNetworkInstall(c.Dir, ch.Argv); err != nil {
					out = append(out, pipeline.CheckResult{Name: ch.Name, Cmd: strings.Join(ch.Argv, " "), ExitCode: -1, Passed: false,
						Output: "network install refused before running: " + err.Error()})
					continue
				}
			}
			sb := *c.Sandbox
			sb.Network = ch.Network
			wrapped, err := sb.Wrap(ch.Argv)
			if err != nil {
				return out, err
			}
			argv = wrapped
		}
		cctx, cancel := context.WithTimeout(ctx, to)
		cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
		cmd.Dir = c.Dir
		cmd.Env = minimalEnv(c.Dir)
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		err := cmd.Run()
		cancel()
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		r := pipeline.CheckResult{Name: ch.Name, Cmd: strings.Join(ch.Argv, " "), Output: tailBytes(buf.Bytes(), 8000), Passed: err == nil}
		var ee *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &ee):
			r.ExitCode = ee.ExitCode()
			// A non-zero exit is a failure. allow_exit is refused at config
			// load and ignored here, so a stale config cannot weaken a gate.
		default:
			r.ExitCode = -1
			r.Output = strings.TrimSpace(r.Output + "\n" + err.Error())
		}
		if isPytest(ch.Argv) && r.ExitCode == pytestNoTests {
			r.Note = "pytest exit 5 means no tests were collected; an empty suite is a failure"
		}
		if r.Passed && zeroTestMarkers.MatchString(r.Output) {
			r.Passed, r.Note = false, "the runner reported that no tests ran; an empty suite is a failure"
		}
		out = append(out, r)
	}
	return out, nil
}

func tailBytes(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

// WTWorkspace adapts a worktree to pipeline.Workspace.
type WTWorkspace struct{ WT worktree.Worktree }

func (w WTWorkspace) ChangedFiles(ctx context.Context) ([]string, error) {
	return worktree.ChangedFiles(ctx, w.WT)
}

func (w WTWorkspace) Diff(ctx context.Context) (string, error) { return worktree.Diff(ctx, w.WT) }

func (w WTWorkspace) DiffCheck(ctx context.Context) (string, bool, error) {
	return worktree.DiffCheck(ctx, w.WT)
}
