package handoff

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
)

var run = Run{ID: "20261106T010203.000Z", CardKey: "BII-655", Title: "Local key bootstrap", Repo: "birunidev-space",
	Branch: "autopilot/BII-655", PRURL: "https://github.com/birunidev/birunidev-space/pull/42", When: time.Date(2026, 11, 6, 1, 2, 3, 0, time.UTC)}

func passed() pipeline.Outcome {
	return pipeline.Outcome{
		Kind:    pipeline.ReadyForReview,
		Summary: "passed sandboxed checks and gates in round 2; awaiting external review",
		Preflight: pipeline.Preflight{Ready: true, AcceptanceCriteria: []string{"key shown once", "revoked key rejected"},
			ScopePaths: []string{"backend/app/auth/**"}},
		Rounds: []pipeline.Round{
			{N: 1, Gates: []pipeline.Finding{{Severity: "blocker", Issue: "check pytest failed"}}},
			{N: 2, Implementer: "Added key hashing.\nUnsure about rotation TTL.", Files: []string{"backend/app/auth/keys.py"},
				Checks: []pipeline.CheckResult{{Name: "pytest", Cmd: "pytest -q", Passed: true, Output: "SECRET_TOKEN=abc 12 passed"}}, DiffCheckOK: true},
		},
	}
}

func TestHandoffComment(t *testing.T) {
	c := HandoffComment(run, passed())
	for _, want := range []string{
		"[AUTOPILOT HANDOFF — 2026-11-06 01:02 UTC | run 20261106T010203.000Z]",
		run.PRURL, "NOT Done", "`backend/app/auth/keys.py`", "`pytest -q` — PASS",
		"1. key shown once", "not independently verified", "round 1: failed", "round 2: passed",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("handoff missing %q\n%s", want, c)
		}
	}
	if strings.Contains(c, "SECRET_TOKEN") {
		t.Fatal("command output leaked into a card comment")
	}
}

func TestBlockedComment(t *testing.T) {
	o := passed()
	o.Kind, o.Summary, o.Err = pipeline.Blocked, "still failing after 3 review rounds", errors.New("per-card token cap reached")
	o.Rounds[1].Gates = []pipeline.Finding{{Severity: "blocker", File: "a.py", Issue: "criterion 2 not met"}}
	r := run
	r.PRURL = ""
	c := BlockedComment(r, o)
	for _, want := range []string{"[AUTOPILOT BLOCKED", "still failing", "per-card token cap", "criterion 2 not met", "not pushed"} {
		if !strings.Contains(c, want) {
			t.Errorf("blocked comment missing %q\n%s", want, c)
		}
	}
	if strings.Contains(c, "SECRET_TOKEN") {
		t.Fatal("command output leaked")
	}
}

func TestClarificationAndPromptFile(t *testing.T) {
	p := pipeline.Preflight{PolicyBlock: "Implementation remains PAUSED until BII-638", Questions: []string{"Is BII-638 done?", "Which key TTL?"},
		ChatGPTPrompt: "Help me turn BII-655 into a spec. ```inject```"}
	dir := t.TempDir()
	path, err := WriteChatGPTPrompt(dir, run, p)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 || filepath.Dir(path) != dir {
		t.Fatalf("prompt file %s mode %v", path, fi.Mode().Perm())
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "Which key TTL?") || !strings.Contains(string(body), "Help me turn BII-655") {
		t.Fatalf("prompt file:\n%s", body)
	}
	c := ClarificationComment(run, p, path)
	for _, want := range []string{"NEEDS CLARIFICATION", "PAUSED until BII-638", "2. Which key TTL?", "Paste-ready ChatGPT prompt", path} {
		if !strings.Contains(c, want) {
			t.Errorf("clarification missing %q", want)
		}
	}
	if strings.Count(c, "```") != 2 {
		t.Fatal("prompt text broke out of its code block")
	}
	if p2, _ := WriteChatGPTPrompt(dir, run, pipeline.Preflight{}); p2 != "" {
		t.Fatal("empty prompt should not write a file")
	}
}

func TestPRBodyAndTitle(t *testing.T) {
	b := PRBody(run, passed())
	for _, want := range []string{"**draft**", "Not merged, not deployed", "- [ ] key shown once", "Scope: backend/app/auth/**"} {
		if !strings.Contains(b, want) {
			t.Errorf("PR body missing %q", want)
		}
	}
	if tt := PRTitle(Run{CardKey: "BII-1", Title: strings.Repeat("x", 300)}); len([]rune(tt)) > 121 {
		t.Fatalf("title too long: %d", len(tt))
	}
	if m := CommitMessage(run, passed()); !strings.HasPrefix(m, "BII-655: Local key bootstrap") {
		t.Fatalf("commit message %q", m)
	}
}
