package main

import (
	"testing"
	"time"
)

func TestClarificationContextFromRunLog(t *testing.T) {
	o, srv := fixture(t)
	mod := time.Date(2026, 11, 7, 11, 25, 0, 0, time.UTC)
	detail := map[string]any{
		"summary":     "Missing accepted test contract",
		"policyBlock": "BII-638 requires source clearance",
		"questions":   []string{"Which import paths are approved?", "Who owns the routing entrypoint?"},
	}
	writeRun(t, o.home, "BII-QUESTION", "r-question", mod,
		end("BII-QUESTION", "r-question", "needs-clarification", "2026-11-07T11:25:00Z",
			map[string]any{"clarification": detail}))
	writeRun(t, o.home, "BII-OLD", "r-old", mod,
		end("BII-OLD", "r-old", "needs-clarification", "2026-11-07T11:24:00Z", nil))
	put(t, o.home, "status.json", map[string]any{"state": "idle", "updatedAt": "2026-11-07T11:55:00Z"})

	s := getSnapshot(t, srv)
	if len(s.TodayOutcomes) != 2 {
		t.Fatalf("outcomes = %+v", s.TodayOutcomes)
	}
	found := false
	for _, out := range s.TodayOutcomes {
		switch out.Card {
		case "BII-QUESTION":
			found = true
			if out.Clarification == nil ||
				out.Clarification.Summary != "Missing accepted test contract" ||
				out.Clarification.PolicyBlock != "BII-638 requires source clearance" ||
				len(out.Clarification.Questions) != 2 ||
				out.Clarification.Questions[1] != "Who owns the routing entrypoint?" {
				t.Errorf("clarification lost: %+v", out)
			}
		case "BII-OLD":
			if out.Clarification != nil {
				t.Errorf("legacy log must not invent clarification: %+v", out)
			}
		}
	}
	if !found {
		t.Fatal("clarification outcome was not returned")
	}
}

func TestMalformedClarificationIsNotTreatedAsVerifiedContext(t *testing.T) {
	o, srv := fixture(t)
	mod := time.Date(2026, 11, 7, 11, 25, 0, 0, time.UTC)
	writeRun(t, o.home, "BII-MALFORMED", "r-malformed", mod,
		end("BII-MALFORMED", "r-malformed", "needs-clarification", "2026-11-07T11:25:00Z",
			map[string]any{"clarification": "fake policy approval"}))
	put(t, o.home, "status.json", map[string]any{"state": "idle"})
	s := getSnapshot(t, srv)
	if len(s.TodayOutcomes) != 1 || s.TodayOutcomes[0].Clarification != nil {
		t.Fatalf("malformed clarification must be unavailable: %+v", s.TodayOutcomes)
	}
}
