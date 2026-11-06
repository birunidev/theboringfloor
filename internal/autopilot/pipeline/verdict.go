package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Preflight is the pre-flight agent's structured answer.
type Preflight struct {
	Ready              bool     `json:"ready"`
	PolicyBlock        string   `json:"policy_block"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	ScopePaths         []string `json:"scope_paths"`
	Questions          []string `json:"questions"`
	ChatGPTPrompt      string   `json:"chatgpt_prompt"`
	Summary            string   `json:"summary"`
}

// Finding is one reviewer finding.
type Finding struct {
	Severity string `json:"severity"` // blocker, major, minor
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Issue    string `json:"issue"`
}

// Verdict is a reviewer's structured answer.
type Verdict struct {
	Reviewer string    `json:"-"`
	Verdict  string    `json:"verdict"` // pass, fail
	Findings []Finding `json:"findings"`
	Summary  string    `json:"summary"`
}

// Passed reports a pass.
func (v Verdict) Passed() bool { return v.Verdict == "pass" }

var errNoJSON = errors.New("no JSON object in output")

// extractJSON returns the JSON object in an agent reply: a ```json fenced
// block if present, else the outermost {...}.
func extractJSON(text string) ([]byte, error) {
	if i := strings.Index(text, "```json"); i >= 0 {
		rest := text[i+len("```json"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			return []byte(strings.TrimSpace(rest[:j])), nil
		}
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, errNoJSON
	}
	return []byte(text[start : end+1]), nil
}

func strictDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON object")
	}
	return nil
}

// ParsePreflight parses and validates a pre-flight reply.
func ParsePreflight(text string) (Preflight, error) {
	var p Preflight
	data, err := extractJSON(text)
	if err != nil {
		return p, err
	}
	if err := strictDecode(data, &p); err != nil {
		return p, fmt.Errorf("pre-flight JSON: %w", err)
	}
	p.AcceptanceCriteria = nonEmpty(p.AcceptanceCriteria)
	p.ScopePaths = nonEmpty(p.ScopePaths)
	p.Questions = nonEmpty(p.Questions)
	p.PolicyBlock = strings.TrimSpace(p.PolicyBlock)
	switch {
	case p.Ready && p.PolicyBlock != "":
		return p, errors.New("pre-flight: ready with a policy_block")
	case p.Ready && len(p.AcceptanceCriteria) == 0:
		return p, errors.New("pre-flight: ready without acceptance_criteria")
	case p.Ready && len(p.ScopePaths) == 0:
		return p, errors.New("pre-flight: ready without scope_paths")
	case !p.Ready && len(p.Questions) == 0 && p.PolicyBlock == "":
		return p, errors.New("pre-flight: not ready but no questions or policy_block")
	}
	for _, s := range p.ScopePaths {
		if strings.HasPrefix(s, "/") || strings.Contains(s, "..") {
			return p, fmt.Errorf("pre-flight: unsafe scope path %q", s)
		}
	}
	return p, nil
}

// ParseVerdict parses a reviewer reply. Malformed or inconsistent output is
// returned as a failing verdict, never as a pass.
func ParseVerdict(reviewer, text string) Verdict {
	bad := func(why string) Verdict {
		return Verdict{Reviewer: reviewer, Verdict: "fail", Summary: "reviewer output rejected: " + why,
			Findings: []Finding{{Severity: "blocker", Issue: "reviewer output invalid: " + why}}}
	}
	data, err := extractJSON(text)
	if err != nil {
		return bad(err.Error())
	}
	var v Verdict
	if err := strictDecode(data, &v); err != nil {
		return bad(err.Error())
	}
	v.Reviewer = reviewer
	for _, f := range v.Findings {
		switch f.Severity {
		case "blocker", "major", "minor":
		default:
			return bad(fmt.Sprintf("unknown severity %q", f.Severity))
		}
		if strings.TrimSpace(f.Issue) == "" {
			return bad("finding without an issue")
		}
	}
	switch v.Verdict {
	case "pass":
		for _, f := range v.Findings {
			if f.Severity != "minor" {
				return bad("pass with a " + f.Severity + " finding")
			}
		}
	case "fail":
		if len(v.Findings) == 0 {
			return bad("fail without findings")
		}
	default:
		return bad(fmt.Sprintf("verdict %q", v.Verdict))
	}
	return v
}

func nonEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
