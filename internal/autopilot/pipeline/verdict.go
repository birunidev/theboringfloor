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

// Finding is one gate finding (or an external reviewer finding fed back).
type Finding struct {
	Severity string `json:"severity"` // blocker, major, minor
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Issue    string `json:"issue"`
}

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

func nonEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
