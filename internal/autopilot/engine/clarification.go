package engine

import (
	"strings"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
)

// clarificationInfo is a bounded excerpt of the existing pre-flight verdict.
// The full self-contained prompt remains on the Space card/local prompt file;
// it must not be copied into browser telemetry.
type clarificationInfo struct {
	Summary     string   `json:"summary,omitempty"`
	PolicyBlock string   `json:"policyBlock,omitempty"`
	Questions   []string `json:"questions,omitempty"`
}

func clarificationExcerpt(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}

func clarificationEvent(p pipeline.Preflight) clarificationInfo {
	out := clarificationInfo{
		Summary:     clarificationExcerpt(p.Summary, 500),
		PolicyBlock: clarificationExcerpt(p.PolicyBlock, 600),
	}
	for _, q := range p.Questions {
		if len(out.Questions) == 8 {
			break
		}
		if v := clarificationExcerpt(q, 350); v != "" {
			out.Questions = append(out.Questions, v)
		}
	}
	return out
}

func outcomeEventData(out pipeline.Outcome, prURL string) map[string]any {
	data := map[string]any{
		"kind":    out.Kind,
		"summary": out.Summary,
		"rounds":  len(out.Rounds),
		"pr":      prURL,
	}
	if out.Err != nil {
		data["error"] = out.Err.Error()
	}
	if out.Kind == pipeline.NeedsClarification {
		data["clarification"] = clarificationEvent(out.Preflight)
	}
	return data
}
