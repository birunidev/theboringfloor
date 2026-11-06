package handoff

import (
	"fmt"
	"strings"
	"testing"
)

const smallDiff = "diff --git a/backend/tests/unit/test_x.py b/backend/tests/unit/test_x.py\nnew file mode 100644\n--- /dev/null\n+++ b/backend/tests/unit/test_x.py\n@@ -0,0 +1,3 @@\n+def test_x():\n+    assert 1 + 1 == 2\n+# ``` fence in code\n"

func packetRun() Run {
	r := run
	r.GitHubRepo, r.Base, r.Diff = "birunidev/birunidev-space", "bii-504-slice-qa", smallDiff
	return r
}

func TestReviewPacketSections(t *testing.T) {
	o := passed()
	o.Rounds[1].Checks[0].Cmd = "/home/someone/.theboringfloor/autopilot/venvs/x/bin/python -m pytest -q"
	p := ReviewPacket(packetRun(), o)
	if !strings.HasPrefix(p, PacketHeader) {
		t.Fatalf("packet must start with %q", PacketHeader)
	}
	for _, want := range []string{
		"STATUS: READY FOR REVIEW", "NOT merged", "CARD: BII-655 — Local key bootstrap",
		"REPO: birunidev/birunidev-space | BRANCH: autopilot/BII-655 -> BASE: bii-504-slice-qa",
		"PR: " + run.PRURL,
		"## 1. Allowed scope", "- backend/app/auth/**",
		"## 2. Files changed (1 files, +3/-0)",
		"## 3. Acceptance criteria", "AC1. key shown once", "AC2. revoked key rejected",
		"## 4. Checks", "`python -m pytest -q` — PASS",
		"## 5. Independent AI review (round 2 of max 3)", "no test for TTL expiry",
		"## 6. Open doubts", "## 7. What a human/ChatGPT reviewer should check",
		"## 8. Diff (complete)", "+def test_x():",
		"## 9. How to respond", ReviewTag, "Autopilot Queue", "Never move it to Done",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("packet missing %q", want)
		}
	}
	if strings.Contains(p, "/home/someone") {
		t.Fatal("local path leaked into the packet")
	}
	if strings.Count(p, "```") != 2 {
		t.Fatalf("diff fence broke the code block (fences=%d)", strings.Count(p, "```"))
	}
	if strings.Contains(p, "SECRET_TOKEN") {
		t.Fatal("command output leaked into the packet")
	}
}

func TestReviewPacketTruncatesLongDiff(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/x b/x\n--- a/x\n+++ b/x\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "+line %d\n", i)
	}
	r := packetRun()
	r.Diff = b.String()
	p := ReviewPacket(r, passed())
	if !strings.Contains(p, "truncated to the first 400 lines") || strings.Contains(p, "+line 999") {
		t.Fatal("long diff not truncated")
	}
	if !strings.Contains(p, "(1 files, +1000/-0)") {
		t.Fatal("diff stat must count the full diff, not the truncated part")
	}
}

func TestShortCmd(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u/venv/bin/python -m pytest -q": "python -m pytest -q",
		"vendor/bin/pint --test":               "vendor/bin/pint --test",
		"php artisan test":                     "php artisan test",
	} {
		if got := ShortCmd(in); got != want {
			t.Errorf("ShortCmd(%q) = %q, want %q", in, got, want)
		}
	}
}
