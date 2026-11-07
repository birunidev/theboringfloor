// Package pipeline runs one card through the autopilot's coding sprint:
//
//	pre-flight (read-only) -> implementer -> sandboxed checks + deterministic
//	gates (scope, sensitive files, empty diff, git diff --check) -> up to
//	MaxRounds fix rounds -> Ready for review | Blocked
//
// Review is NOT done here. A passing card becomes a draft PR plus an evidence
// packet for external review (founder / ChatGPT / CTO on the board).
//
// It depends only on small interfaces (Agent, Workspace, Checker), so it
// is tested with fakes; cmd/autopilot wires them to opencode and git.
package pipeline

import (
	"context"
	"fmt"
	"strings"
)

// Role tells the Agent which permission profile a session gets.
type Role string

const (
	RolePreflight Role = "preflight" // read-only, Space reads allowed
	RoleImplement Role = "implement" // writes inside scope, allowlisted checks
)

// StageRequest is one agent turn.
type StageRequest struct {
	Role    Role
	Name    string // stage label for logs, e.g. "review:diff-scope"
	Prompt  string
	Session string   // continue this session (implementer fix rounds); empty = new
	Scope   []string // implementer write scope
}

// StageResponse is an agent turn's result.
type StageResponse struct {
	Text    string
	Session string
}

// Agent runs stage turns.
type Agent interface {
	Run(ctx context.Context, req StageRequest) (StageResponse, error)
}

// Workspace exposes the card worktree's changes.
type Workspace interface {
	ChangedFiles(ctx context.Context) ([]string, error)
	Diff(ctx context.Context) (string, error)
	// DiffCheck runs `git diff --check` against the base; ok=false means
	// whitespace errors or conflict markers were found.
	DiffCheck(ctx context.Context) (output string, ok bool, err error)
}

// Checker runs the repository's tests/lint/build deterministically.
type Checker interface {
	Run(ctx context.Context) ([]CheckResult, error)
}

// Config tunes the loop.
type Config struct {
	MaxRounds int // default 3
}

// OutcomeKind is where the card should go next.
type OutcomeKind string

const (
	NeedsClarification OutcomeKind = "needs-clarification"
	ReadyForReview     OutcomeKind = "ready-for-review"
	Blocked            OutcomeKind = "blocked"
)

// Round records one implement+review round.
type Round struct {
	N           int
	Implementer string // implementer's own summary (unverified)
	Files       []string
	Checks      []CheckResult
	DiffCheckOK bool
	DiffCheck   string // `git diff --check` report when not OK
	Gates       []Finding
}

// Passed reports whether the round passed every deterministic gate.
func (r Round) Passed() bool { return len(r.Gates) == 0 }

// Findings returns the round's gate findings.
func (r Round) Findings() []Finding { return append([]Finding(nil), r.Gates...) }

// Outcome is the pipeline's result for one card.
type Outcome struct {
	Kind      OutcomeKind
	Preflight Preflight
	Rounds    []Round
	Summary   string
	Err       error // set when the run was cut short (cap, timeout, kill switch, agent failure)
}

// Last returns the last round, if any.
func (o Outcome) Last() (Round, bool) {
	if len(o.Rounds) == 0 {
		return Round{}, false
	}
	return o.Rounds[len(o.Rounds)-1], true
}

// Run takes one card from pre-flight to an outcome. It never moves cards,
// commits or pushes; the caller acts on the Outcome.
func Run(ctx context.Context, card CardContext, agent Agent, ws Workspace, checker Checker, cfg Config) Outcome {
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 3
	}

	pre, err := agent.Run(ctx, StageRequest{Role: RolePreflight, Name: "preflight", Prompt: PreflightPrompt(card)})
	if err != nil {
		return Outcome{Kind: Blocked, Summary: "pre-flight agent failed", Err: err}
	}
	p, err := ParsePreflight(pre.Text)
	if err != nil {
		return Outcome{Kind: Blocked, Summary: "pre-flight output was invalid; no code was changed", Err: err}
	}
	if !p.Ready {
		sum := "card is not ready to implement; no code was changed"
		if p.PolicyBlock != "" {
			sum = "card is blocked by policy; no code was changed"
		}
		return Outcome{Kind: NeedsClarification, Preflight: p, Summary: sum}
	}

	out := Outcome{Preflight: p}
	session := ""
	var prev []Finding
	for n := 1; n <= cfg.MaxRounds; n++ {
		if err := ctx.Err(); err != nil {
			return abort(out, "run cut short", err)
		}
		prompt := ImplementPrompt(card, p)
		if n > 1 {
			prompt = FixPrompt(n-1, prev)
		}
		impl, err := agent.Run(ctx, StageRequest{Role: RoleImplement, Name: fmt.Sprintf("implement:%d", n), Prompt: prompt, Session: session, Scope: p.ScopePaths})
		if err != nil {
			return abort(out, fmt.Sprintf("implementer failed in round %d", n), err)
		}
		session = impl.Session
		r := Round{N: n, Implementer: impl.Text}

		if r.Files, err = ws.ChangedFiles(ctx); err != nil {
			return abort(out, "could not read the worktree changes", err)
		}
		diff, err := ws.Diff(ctx)
		if err != nil {
			return abort(out, "could not read the diff", err)
		}
		if r.Checks, err = checker.Run(ctx); err != nil {
			return abort(out, "could not run checks", err)
		}
		r.Gates = CodeGates(r.Files, p.ScopePaths, r.Checks)
		if strings.TrimSpace(diff) == "" && len(r.Files) > 0 {
			r.Gates = append(r.Gates, Finding{Severity: "blocker", Issue: "files changed but the diff is empty; evidence would be incomplete"})
		}
		check, ok, err := ws.DiffCheck(ctx)
		if err != nil {
			return abort(out, "could not run git diff --check", err)
		}
		r.DiffCheckOK, r.DiffCheck = ok, check
		if !ok {
			r.Gates = append(r.Gates, Finding{Severity: "blocker", Issue: "git diff --check reported problems: " + tail(check, 600)})
		}
		out.Rounds = append(out.Rounds, r)
		if r.Passed() {
			out.Kind = ReadyForReview
			out.Summary = fmt.Sprintf("passed sandboxed checks and gates in round %d; awaiting external review", n)
			return out
		}
		prev = r.Findings()
	}
	out.Kind = Blocked
	out.Summary = fmt.Sprintf("still failing checks or gates after %d rounds", cfg.MaxRounds)
	return out
}

func abort(o Outcome, why string, err error) Outcome {
	o.Kind, o.Summary, o.Err = Blocked, why, err
	return o
}

// FindingsText renders findings as a bullet list for comments and logs.
func FindingsText(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		loc := ""
		if f.File != "" {
			loc = " " + f.File
			if f.Line > 0 {
				loc = fmt.Sprintf(" %s:%d", f.File, f.Line)
			}
		}
		fmt.Fprintf(&b, "- [%s]%s %s\n", f.Severity, loc, f.Issue)
	}
	return b.String()
}
