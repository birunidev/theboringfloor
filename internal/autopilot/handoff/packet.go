package handoff

import (
	"fmt"
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

// ReviewPacket is the standardized, self-contained comment a card carries
// when it reaches Review. It holds everything an external reviewer needs,
// including the diff, so the card alone is enough to review the change.
func ReviewPacket(r Run, o pipeline.Outcome) string {
	last, _ := o.Last()
	files, add, del := DiffStat(r.Diff)
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s | run %s]\n", PacketHeader, r.When.UTC().Format("2006-01-02 15:04 UTC"), r.ID)
	b.WriteString("STATUS: READY FOR REVIEW — draft PR open; NOT merged, NOT deployed, card is in Review (never Done by the autopilot).\n")
	fmt.Fprintf(&b, "CARD: %s — %s\n", r.CardKey, oneLine(r.Title))
	fmt.Fprintf(&b, "REPO: %s | BRANCH: %s -> BASE: %s\n", r.GitHubRepo, r.Branch, r.Base)
	fmt.Fprintf(&b, "PR: %s\n", r.PRURL)
	fmt.Fprintf(&b, "RESULT: %s\n", o.Summary)

	b.WriteString("\n## 1. Allowed scope\n")
	for _, s := range o.Preflight.ScopePaths {
		fmt.Fprintf(&b, "- %s\n", s)
	}

	fmt.Fprintf(&b, "\n## 2. Files changed (%d files, +%d/-%d)\n", files, add, del)
	for _, f := range last.Files {
		fmt.Fprintf(&b, "- %s\n", f)
	}

	b.WriteString("\n## 3. Acceptance criteria (as agreed in pre-flight)\n")
	for i, ac := range o.Preflight.AcceptanceCriteria {
		fmt.Fprintf(&b, "AC%d. %s\n", i+1, oneLine(ac))
	}

	b.WriteString("\n## 4. Checks run by the autopilot (bubblewrap sandbox: no network, home hidden)\n")
	b.WriteString(checksLines(last.Checks))

	fmt.Fprintf(&b, "\n## 5. Independent AI review (round %d of max 3)\n", last.N)
	b.WriteString(reviewerLines(last.Verdicts))
	b.WriteString("\nRounds:\n" + roundHistory(o))

	b.WriteString("\n## 6. Open doubts\n" + doubts(o))

	b.WriteString("\n## 7. What a human/ChatGPT reviewer should check\n")
	b.WriteString("- Each AC is really proven by the diff (not just mentioned in a test name).\n")
	b.WriteString("- Tests assert behavior and would fail if the code were wrong; nothing skipped, weakened or mocked away.\n")
	b.WriteString("- Only allowed-scope files changed; no app code, secrets, CI, migrations or dependency changes unless the card asked.\n")
	b.WriteString("- Fits the card's latest OVERRIDE/FOUNDER sections and the board policy.\n")

	d, cut := packetDiff(r.Diff)
	if cut {
		fmt.Fprintf(&b, "\n## 8. Diff (truncated to the first %d lines; full diff in the PR)\n", maxPacketDiffLines)
	} else {
		b.WriteString("\n## 8. Diff (complete)\n")
	}
	b.WriteString("```diff\n" + d + "\n```\n")

	b.WriteString("\n## 9. How to respond\n")
	fmt.Fprintf(&b, "Post ONE comment on this card starting with `%s`, using the format in the master review prompt: VERDICT (APPROVE | REQUEST_CHANGES | NEEDS_FOUNDER), per-AC evidence, findings. ", ReviewTag)
	b.WriteString("REQUEST_CHANGES -> move the card to \"Autopilot Queue\" (the autopilot will fix it on the same branch/PR). APPROVE or NEEDS_FOUNDER -> leave it in Review for the founder. Never move it to Done, never merge.\n")
	b.WriteString(footer)
	return b.String()
}
