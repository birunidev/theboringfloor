package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
)

func TestClarificationEndEventIncludesActualQuestions(t *testing.T) {
	out := pipeline.Outcome{
		Kind: pipeline.NeedsClarification,
		Summary: "card is not ready to implement; no code was changed",
		Preflight: pipeline.Preflight{
			Summary:       "Missing source authorization for the frontend import",
			PolicyBlock:   "BII-638 source allowlist is still pending",
			Questions:     []string{"Can the original CSS be independently rewritten?", "Who owns main.tsx?"},
			ChatGPTPrompt: "PRIVATE PROMPT MUST NOT APPEAR IN BROWSER TELEMETRY",
		},
	}
	event := outcomeEventData(out, "")
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"clarification", "Missing source authorization", "BII-638 source allowlist",
		"Can the original CSS", "Who owns main.tsx?",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %q in emitted event: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "PRIVATE PROMPT") {
		t.Fatal("the full ChatGPT prompt must not be emitted to the office")
	}
}

func TestClarificationExcerptsAreBoundedAndNormalized(t *testing.T) {
	questions := make([]string, 12)
	for i := range questions {
		questions[i] = strings.Repeat("é", 500)
	}
	p := pipeline.Preflight{
		Summary:     "  Missing\n  evidence\t in the card ",
		PolicyBlock: strings.Repeat("B", 700),
		Questions:   questions,
	}
	got := clarificationEvent(p)
	if got.Summary != "Missing evidence in the card" {
		t.Errorf("bad normalized summary: %q", got.Summary)
	}
	if len([]rune(got.PolicyBlock)) > 601 {
		t.Fatal("policy reason not bounded")
	}
	if len(got.Questions) != 8 {
		t.Fatalf("got %d questions, want at most 8", len(got.Questions))
	}
	for _, q := range got.Questions {
		if len([]rune(q)) > 351 {
			t.Error("question not bounded by rune count")
		}
	}
}

func TestNonClarificationOutcomesDoNotInventQuestions(t *testing.T) {
	for _, kind := range []pipeline.OutcomeKind{pipeline.Blocked, pipeline.ReadyForReview} {
		m := outcomeEventData(pipeline.Outcome{Kind: kind, Summary: "ordinary outcome"}, "https://github.com/example/example/pull/1")
		if _, ok := m["clarification"]; ok {
			t.Fatalf("outcome %s unexpectedly contains clarification data", kind)
		}
	}
}
