package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// Owns runs before any check and fails the run when the worktree holds a
	// change outside the profile's allowed paths.
	Owns func(ctx context.Context) error
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
	if c.Owns != nil {
		if err := c.Owns(ctx); err != nil {
			return []pipeline.CheckResult{{Name: "profile source ownership", Cmd: "verify changed files against the profile", ExitCode: -1, Passed: false, Output: err.Error()}}, nil
		}
	}
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
		// Test runners colour their summaries even when CI=1 (Vitest does), and
		// an escape code in front of "Tests" defeats every anchored pattern.
		// Match and report on the plain text.
		clean := stripANSI(buf.String())
		r := pipeline.CheckResult{Name: ch.Name, Cmd: strings.Join(ch.Argv, " "), Output: tailBytes([]byte(clean), 8000), Passed: err == nil}
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
		if r.Passed {
			if why := outputAssertion(ch, clean); why != "" {
				r.Passed, r.Note = false, why
			}
		}
		if r.Passed && zeroTestMarkers.MatchString(r.Output) {
			r.Passed, r.Note = false, "the runner reported that no tests ran; an empty suite is a failure"
		}
		out = append(out, r)
	}
	return out, nil
}

// outputAssertion applies a check's require_output / reject_output to its full
// output and returns why the check must fail, or "".
func outputAssertion(ch Check, output string) string {
	if ch.RejectOutput != "" {
		re, err := regexp.Compile(ch.RejectOutput)
		if err != nil {
			return "reject_output is not a valid regular expression: " + err.Error()
		}
		if m := re.FindString(output); m != "" {
			return fmt.Sprintf("output matched reject_output %q: %q", ch.RejectOutput, strings.TrimSpace(m))
		}
	}
	if ch.RequireOutput != "" {
		re, err := regexp.Compile(ch.RequireOutput)
		if err != nil {
			return "require_output is not a valid regular expression: " + err.Error()
		}
		if !re.MatchString(output) {
			return fmt.Sprintf("output did not match require_output %q (the evidence this gate needs is missing)", ch.RequireOutput)
		}
	}
	return ""
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

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
