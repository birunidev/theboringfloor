package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
}

// Run implements pipeline.Checker. A command that cannot start counts as a
// failed check, not an infrastructure error.
func (c CmdChecker) Run(ctx context.Context) ([]pipeline.CheckResult, error) {
	to := c.Timeout
	if to <= 0 {
		to = 15 * time.Minute
	}
	var out []pipeline.CheckResult
	for _, ch := range c.Checks {
		if len(ch.Argv) == 0 {
			continue
		}
		argv := ch.Argv
		if !c.Unsandboxed {
			if c.Sandbox == nil {
				return out, ErrNoSandbox
			}
			wrapped, err := c.Sandbox.Wrap(ch.Argv)
			if err != nil {
				return out, err
			}
			argv = wrapped
		}
		cctx, cancel := context.WithTimeout(ctx, to)
		cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
		cmd.Dir = c.Dir
		cmd.Env = append(os.Environ(), "CI=1")
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
			for _, ok := range ch.AllowExit {
				if ok == r.ExitCode {
					r.Passed = true
					r.Note = fmt.Sprintf("exit %d accepted (configured)", r.ExitCode)
				}
			}
		default:
			r.ExitCode = -1
			r.Output = strings.TrimSpace(r.Output + "\n" + err.Error())
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
