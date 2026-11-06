// Package handoff renders what the autopilot writes back: the card
// comments (handoff, blocked, needs-clarification), the draft PR body, and
// the paste-ready ChatGPT clarification prompt file.
//
// Card comments carry check names and pass/fail only, never command
// output: output can contain secrets and lives in the local run log.
package handoff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
)

// Run identifies one autopilot run for headers and links.
type Run struct {
	ID      string
	CardKey string
	Title   string
	Repo    string
	Branch  string
	PRURL   string
	LogPath string
	When    time.Time
}

func header(kind string, r Run) string {
	return fmt.Sprintf("[AUTOPILOT %s — %s | run %s]", kind, r.When.UTC().Format("2006-01-02 15:04 UTC"), r.ID)
}

const footer = "\n\nAutopilot rules: branch + draft PR only; never pushes to main, merges, deploys or moves cards to Done. Founder review decides."

func checksLines(cs []pipeline.CheckResult) string {
	if len(cs) == 0 {
		return "- (no check commands configured)\n"
	}
	var b strings.Builder
	for _, c := range cs {
		st := "PASS"
		if !c.Passed {
			st = fmt.Sprintf("FAIL (exit %d)", c.ExitCode)
		}
		fmt.Fprintf(&b, "- `%s` — %s\n", c.Cmd, st)
	}
	return b.String()
}

func reviewerLines(vs []pipeline.Verdict) string {
	var b strings.Builder
	for _, v := range vs {
		fmt.Fprintf(&b, "- %s: **%s** — %s\n", v.Reviewer, v.Verdict, oneLine(v.Summary))
		for _, f := range v.Findings {
			loc := ""
			if f.File != "" {
				loc = " " + f.File
				if f.Line > 0 {
					loc = fmt.Sprintf(" %s:%d", f.File, f.Line)
				}
			}
			fmt.Fprintf(&b, "  - [%s]%s %s\n", f.Severity, loc, oneLine(f.Issue))
		}
	}
	return b.String()
}

func roundHistory(o pipeline.Outcome) string {
	var b strings.Builder
	for _, r := range o.Rounds {
		status := "failed"
		if r.Passed() {
			status = "passed"
		}
		fmt.Fprintf(&b, "- round %d: %s (%d gate findings, %d blocker/major findings)\n", r.N, status, len(r.Gates), len(r.Findings())-len(r.Gates))
	}
	return b.String()
}

func doubts(o pipeline.Outcome) string {
	last, ok := o.Last()
	if !ok {
		return "- none recorded\n"
	}
	var b strings.Builder
	for _, v := range last.Verdicts {
		for _, f := range v.Findings {
			if f.Severity == "minor" {
				fmt.Fprintf(&b, "- (%s) %s\n", v.Reviewer, oneLine(f.Issue))
			}
		}
	}
	if s := strings.TrimSpace(last.Implementer); s != "" {
		fmt.Fprintf(&b, "- implementer's own notes (not independently verified): %s\n", oneLine(clip(s, 700)))
	}
	if b.Len() == 0 {
		return "- none raised\n"
	}
	return b.String()
}

// HandoffComment is posted when a card passed and its draft PR is open.
func HandoffComment(r Run, o pipeline.Outcome) string {
	last, _ := o.Last()
	var b strings.Builder
	b.WriteString(header("HANDOFF", r) + "\n")
	fmt.Fprintf(&b, "Ready for founder review: %s\nDraft PR: %s\nBranch: `%s` (%s) — moved to Review, NOT Done.\n\n", o.Summary, r.PRURL, r.Branch, r.Repo)
	b.WriteString("**What changed**\n")
	for _, f := range last.Files {
		fmt.Fprintf(&b, "- `%s`\n", f)
	}
	b.WriteString("\n**How it was verified** (commands run by the autopilot itself)\n")
	b.WriteString(checksLines(last.Checks))
	b.WriteString("\n**Acceptance criteria checked**\n")
	for i, ac := range o.Preflight.AcceptanceCriteria {
		fmt.Fprintf(&b, "%d. %s\n", i+1, oneLine(ac))
	}
	b.WriteString("\n**What reviewers flagged** (final round)\n")
	b.WriteString(reviewerLines(last.Verdicts))
	b.WriteString("\n**Review rounds**\n" + roundHistory(o))
	b.WriteString("\n**Open doubts**\n" + doubts(o))
	b.WriteString(footer)
	return b.String()
}

// BlockedComment is posted when a card failed review or the run was cut short.
func BlockedComment(r Run, o pipeline.Outcome) string {
	var b strings.Builder
	b.WriteString(header("BLOCKED", r) + "\n")
	fmt.Fprintf(&b, "Moved to Blocked: %s.\n", o.Summary)
	if o.Err != nil {
		fmt.Fprintf(&b, "Reason: %s\n", oneLine(clip(o.Err.Error(), 400)))
	}
	if r.Branch != "" {
		fmt.Fprintf(&b, "Work in progress is kept on local branch `%s` (not pushed).\n", r.Branch)
	}
	if last, ok := o.Last(); ok {
		b.WriteString("\n**Last round's blocker/major findings**\n")
		fs := last.Findings()
		if len(fs) == 0 {
			b.WriteString("- none (the run stopped before review finished)\n")
		}
		for _, f := range fs {
			fmt.Fprintf(&b, "- [%s] %s %s\n", f.Severity, f.File, oneLine(clip(f.Issue, 400)))
		}
		b.WriteString("\n**Checks**\n" + checksLines(last.Checks))
		b.WriteString("\n**Review rounds**\n" + roundHistory(o))
	}
	b.WriteString("\nNext step: founder decides — clarify the card, split it, or take it over by hand.")
	b.WriteString(footer)
	return b.String()
}

// ClarificationComment is posted when pre-flight found the card not ready.
func ClarificationComment(r Run, p pipeline.Preflight, promptPath string) string {
	var b strings.Builder
	b.WriteString(header("NEEDS CLARIFICATION", r) + "\n")
	b.WriteString("Pre-flight stopped before any code change.\n")
	if p.PolicyBlock != "" {
		fmt.Fprintf(&b, "\n**Blocked by policy:** %s\n", oneLine(clip(p.PolicyBlock, 600)))
	}
	if len(p.Questions) > 0 {
		b.WriteString("\n**Questions for the founder**\n")
		for i, q := range p.Questions {
			fmt.Fprintf(&b, "%d. %s\n", i+1, oneLine(q))
		}
	}
	if s := strings.TrimSpace(p.ChatGPTPrompt); s != "" {
		b.WriteString("\n**Paste-ready ChatGPT prompt**\n```\n")
		b.WriteString(strings.ReplaceAll(s, "```", "'''"))
		b.WriteString("\n```\n")
	}
	if promptPath != "" {
		fmt.Fprintf(&b, "(Also saved locally: %s)\n", promptPath)
	}
	b.WriteString("\nTo retry: answer the questions on the card, move it back to the ready list and keep the autopilot label.")
	b.WriteString(footer)
	return b.String()
}

// PRTitle is the draft PR title.
func PRTitle(r Run) string {
	return clip(fmt.Sprintf("%s: %s", r.CardKey, oneLine(r.Title)), 120)
}

// PRBody is the draft PR description.
func PRBody(r Run, o pipeline.Outcome) string {
	last, _ := o.Last()
	var b strings.Builder
	fmt.Fprintf(&b, "Opened as a **draft** by the theboringfloor autopilot for card **%s**. Not merged, not deployed; the card is in Review, not Done.\n\n", r.CardKey)
	b.WriteString("## Acceptance criteria\n")
	for _, ac := range o.Preflight.AcceptanceCriteria {
		fmt.Fprintf(&b, "- [ ] %s\n", oneLine(ac))
	}
	b.WriteString("\n## Verification (run by the autopilot)\n" + checksLines(last.Checks))
	b.WriteString("\n## Independent review (final round)\n" + reviewerLines(last.Verdicts))
	b.WriteString("\n## Review rounds\n" + roundHistory(o))
	b.WriteString("\n## Open doubts\n" + doubts(o))
	fmt.Fprintf(&b, "\nScope: %s\nRun: `%s`\n", strings.Join(o.Preflight.ScopePaths, ", "), r.ID)
	return b.String()
}

// CommitMessage is the autopilot's commit message.
func CommitMessage(r Run, o pipeline.Outcome) string {
	return fmt.Sprintf("%s: %s\n\nAutopilot run %s. %s\nCard: %s", r.CardKey, oneLine(clip(r.Title, 72)), r.ID, o.Summary, r.CardKey)
}

// WriteChatGPTPrompt saves the clarification prompt next to the run log
// and returns its path.
func WriteChatGPTPrompt(dir string, r Run, p pipeline.Preflight) (string, error) {
	if strings.TrimSpace(p.ChatGPTPrompt) == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.ID+"-chatgpt-prompt.md")
	var b strings.Builder
	fmt.Fprintf(&b, "# Clarify %s — %s\n\n", r.CardKey, oneLine(r.Title))
	if p.PolicyBlock != "" {
		fmt.Fprintf(&b, "Blocked by policy: %s\n\n", p.PolicyBlock)
	}
	for i, q := range p.Questions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, q)
	}
	b.WriteString("\n---\n\n" + strings.TrimSpace(p.ChatGPTPrompt) + "\n")
	return path, os.WriteFile(path, []byte(b.String()), 0o600)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
