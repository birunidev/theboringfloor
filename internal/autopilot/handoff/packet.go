package handoff

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
)

// PacketHeader starts every review packet. Reviewers (ChatGPT, the founder)
// look for the newest comment starting with it.
const PacketHeader = "[AUTOPILOT REVIEW PACKET v1"

// ReviewTag starts an external reviewer's answer.
const ReviewTag = "[CHATGPT REVIEW v1]"

const (
	maxPacketDiffLines = 400
	maxPacketDiffChars = 24000
)

// DiffStat counts files, added and removed lines in a unified diff.
func DiffStat(diff string) (files, added, removed int) {
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			files++
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return
}

func packetDiff(diff string) (string, bool) {
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	cut := false
	if len(lines) > maxPacketDiffLines {
		lines, cut = lines[:maxPacketDiffLines], true
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxPacketDiffChars {
		out, cut = out[:maxPacketDiffChars], true
	}
	// A fence inside the diff must not close the packet's code block.
	return strings.ReplaceAll(out, "```", "'''"), cut
}

// secretPatterns redact token-shaped strings from check output before it is
// posted. Checks already run with a cleared environment in the sandbox; this
// is a second guard.
var secretPatterns = regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|bearer\s+[A-Za-z0-9._~+/=-]{20,}|-----BEGIN [A-Z ]*PRIVATE KEY-----)`)

// Redact masks token-shaped strings.
func Redact(s string) string { return secretPatterns.ReplaceAllString(s, "[REDACTED]") }

func outputTail(s string, lines int) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > lines {
		ls = append([]string{fmt.Sprintf("… (%d earlier lines omitted)", len(ls)-lines)}, ls[len(ls)-lines:]...)
	}
	return strings.ReplaceAll(Redact(strings.Join(ls, "\n")), "```", "'''")
}

func orUnavailable(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Unavailable"
	}
	return s
}

// ReviewPacket is the standardized, self-contained evidence comment a card
// carries when it reaches Review. The autopilot does no review itself; this
// packet is what the external reviewer (founder / ChatGPT / CTO) judges.
func ReviewPacket(r Run, o pipeline.Outcome) string {
	last, _ := o.Last()
	files, add, del := DiffStat(r.Diff)
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s | run %s]\n", PacketHeader, r.When.UTC().Format("2006-01-02 15:04 UTC"), r.ID)
	b.WriteString("STATUS: READY FOR EXTERNAL REVIEW — draft PR open; NOT merged, NOT deployed, card is in Review (never Done by the autopilot). No AI review was performed by the autopilot.\n")
	fmt.Fprintf(&b, "CARD: %s — %s\n", r.CardKey, oneLine(r.Title))
	fmt.Fprintf(&b, "REPO: %s | BRANCH: %s -> BASE: %s\n", orUnavailable(r.GitHubRepo), r.Branch, r.Base)
	fmt.Fprintf(&b, "BASE SHA: %s | HEAD SHA: %s\n", orUnavailable(r.BaseSHA), orUnavailable(r.HeadSHA))
	fmt.Fprintf(&b, "PR: %s\n", r.PRURL)
	fmt.Fprintf(&b, "RESULT: %s\n", o.Summary)

	b.WriteString("\n## 1. Allowed scope\n")
	for _, s := range o.Preflight.ScopePaths {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	fmt.Fprintf(&b, "\n## 2. Files changed (%d files, +%d/-%d, base..head)\n", files, add, del)
	for _, f := range last.Files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	b.WriteString("\n## 3. Acceptance criteria (as agreed in pre-flight)\n")
	for i, ac := range o.Preflight.AcceptanceCriteria {
		fmt.Fprintf(&b, "AC%d. %s\n", i+1, oneLine(ac))
	}
	b.WriteString("\n## 4. Checks run by the autopilot (bubblewrap sandbox: no network, home hidden, cleared environment)\n")
	b.WriteString(checksLines(last.Checks))
	for _, c := range last.Checks {
		if strings.TrimSpace(c.Output) == "" {
			continue
		}
		fmt.Fprintf(&b, "\nOutput of `%s` (last 40 lines, token-shaped strings redacted):\n```\n%s\n```\n", ShortCmd(c.Cmd), outputTail(c.Output, 40))
	}
	b.WriteString("\n## 5. git diff --check (base..worktree, incl. new files)\n")
	if last.DiffCheckOK {
		b.WriteString("PASS — no whitespace errors or conflict markers\n")
	} else {
		fmt.Fprintf(&b, "FAIL\n```\n%s\n```\n", outputTail(last.DiffCheck, 40))
	}
	b.WriteString("\n## 6. Rounds (deterministic gates only)\n" + roundHistory(o))
	b.WriteString("\n## 7. Implementer notes (not verified)\n" + doubts(o))
	b.WriteString("\n## 8. What the external reviewer should check\n")
	b.WriteString("- Each AC is really proven by the diff (not just mentioned in a test name).\n")
	b.WriteString("- Tests assert behavior and would fail if the code were wrong; nothing skipped, weakened or mocked away.\n")
	b.WriteString("- Only allowed-scope files changed; no app code, secrets, CI, migrations or dependency changes unless the card asked.\n")
	b.WriteString("- Fits the card's latest OVERRIDE/FOUNDER sections and the board policy.\n")
	d, cut := packetDiff(r.Diff)
	if strings.TrimSpace(d) == "" {
		b.WriteString("\n## 9. Diff\nUnavailable — the diff could not be captured; do not approve without reading the PR.\n")
	} else {
		if cut {
			fmt.Fprintf(&b, "\n## 9. Diff (truncated to the first %d lines; full diff in the PR)\n", maxPacketDiffLines)
		} else {
			b.WriteString("\n## 9. Diff (complete, base..head)\n")
		}
		b.WriteString("```diff\n" + d + "\n```\n")
	}
	b.WriteString("\n## 10. How to respond\n")
	fmt.Fprintf(&b, "Post ONE comment on this card starting with `%s`: VERDICT (APPROVE | REQUEST_CHANGES | NEEDS_FOUNDER), per-AC evidence, findings. ", ReviewTag)
	b.WriteString("REQUEST_CHANGES -> move the card to \"Autopilot Queue\" (the autopilot fixes it on the same branch/PR). APPROVE or NEEDS_FOUNDER -> leave it in Review for the founder. Never move it to Done, never merge.\n")
	b.WriteString(footer)
	return b.String()
}
