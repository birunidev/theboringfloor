// Package pipeline runs one card through the autopilot's stages:
//
//	pre-flight (read-only) -> implementer -> code gates + 3 independent
//	reviewers -> up to MaxRounds fix rounds -> Ready for review | Blocked
//
// It depends only on small interfaces (Agent, Workspace, Checker), so it
// is tested with fakes; cmd/autopilot wires them to opencode and git.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Role tells the Agent which permission profile a session gets.
type Role string

const (
	RolePreflight Role = "preflight" // read-only, Space reads allowed
	RoleImplement Role = "implement" // writes inside scope, allowlisted checks
	RoleReview    Role = "review"    // read-only, no Space
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

// Agent runs stage turns. Implementations must be safe for concurrent use:
// the three reviewers run in parallel, each in its own fresh session.
type Agent interface {
	Run(ctx context.Context, req StageRequest) (StageResponse, error)
}

// Workspace exposes the card worktree's changes.
type Workspace interface {
	ChangedFiles(ctx context.Context) ([]string, error)
	Diff(ctx context.Context) (string, error)
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
	Gates       []Finding
	Verdicts    []Verdict
}

// Passed reports whether the round passed every gate and reviewer.
func (r Round) Passed() bool {
	if len(r.Gates) > 0 || len(r.Verdicts) == 0 {
		return false
	}
	for _, v := range r.Verdicts {
		if !v.Passed() {
			return false
		}
	}
	return true
}

// Findings returns blocker/major findings of the round (gates first).
func (r Round) Findings() []Finding {
	out := append([]Finding(nil), r.Gates...)
	for _, v := range r.Verdicts {
		for _, f := range v.Findings {
			if f.Severity != "minor" {
				f.Issue = "(" + v.Reviewer + ") " + f.Issue
				out = append(out, f)
			}
		}
	}
	return out
}

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

// Reviewers is the fixed review panel.
var Reviewers = []string{ReviewerAC, ReviewerChecks, ReviewerScope}

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

		in := ReviewInput{Card: card, Pre: p, Files: r.Files, Diff: diff, Checks: r.Checks, Round: n, Previous: prev}
		r.Verdicts, err = review(ctx, agent, in)
		if err != nil {
			out.Rounds = append(out.Rounds, r)
			return abort(out, fmt.Sprintf("a reviewer failed in round %d", n), err)
		}
		out.Rounds = append(out.Rounds, r)
		if r.Passed() {
			out.Kind = ReadyForReview
			out.Summary = fmt.Sprintf("passed all gates and %d reviewers in round %d", len(Reviewers), n)
			return out
		}
		prev = r.Findings()
	}
	out.Kind = Blocked
	out.Summary = fmt.Sprintf("still failing after %d review rounds", cfg.MaxRounds)
	return out
}

func abort(o Outcome, why string, err error) Outcome {
	o.Kind, o.Summary, o.Err = Blocked, why, err
	return o
}

// review runs the panel in parallel, each reviewer in a fresh session.
func review(ctx context.Context, agent Agent, in ReviewInput) ([]Verdict, error) {
	verdicts := make([]Verdict, len(Reviewers))
	errs := make([]error, len(Reviewers))
	var wg sync.WaitGroup
	for i, name := range Reviewers {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			res, err := agent.Run(ctx, StageRequest{Role: RoleReview, Name: "review:" + name, Prompt: ReviewPrompt(name, in)})
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", name, err)
				return
			}
			verdicts[i] = ParseVerdict(name, res.Text)
		}(i, name)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return verdicts, err
	}
	return verdicts, nil
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
