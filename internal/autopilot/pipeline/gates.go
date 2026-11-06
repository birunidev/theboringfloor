package pipeline

import (
	"fmt"
	"path"
	"strings"
)

// CheckResult is one deterministic command the autopilot ran itself.
type CheckResult struct {
	Name     string `json:"name"`
	Cmd      string `json:"cmd"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"` // tail, truncated
	Passed   bool   `json:"passed"`
}

// InScope reports whether file matches one of the scope patterns. A pattern
// is a path.Match glob, a directory prefix ending in "/", or "dir/**".
func InScope(file string, scope []string) bool {
	for _, seg := range strings.Split(file, "/") {
		if seg == ".." {
			return false
		}
	}
	file = strings.TrimPrefix(path.Clean("/"+file), "/")
	for _, s := range scope {
		s = strings.TrimPrefix(strings.TrimSpace(s), "./")
		switch {
		case s == "":
		case strings.HasSuffix(s, "/**"):
			if p := strings.TrimSuffix(s, "/**"); file == p || strings.HasPrefix(file, p+"/") {
				return true
			}
		case strings.HasSuffix(s, "/"):
			if strings.HasPrefix(file, s) {
				return true
			}
		default:
			if ok, _ := path.Match(s, file); ok || file == s {
				return true
			}
		}
	}
	return false
}

var sensitive = []string{".env", ".npmrc", ".pypirc", "id_rsa", "id_ed25519", ".pem", ".key", "credentials", "secrets"}

// isSensitive flags files that must never appear in an autopilot diff.
func isSensitive(file string) bool {
	base := strings.ToLower(path.Base(file))
	for _, s := range sensitive {
		if base == s || strings.HasPrefix(base, s+".") || strings.HasSuffix(base, s) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(file), "/.github/workflows/") || strings.HasPrefix(strings.ToLower(file), ".github/workflows/")
}

// CodeGates are the deterministic checks that fail a round regardless of
// what the reviewer agents say.
func CodeGates(files []string, scope []string, checks []CheckResult) []Finding {
	var out []Finding
	if len(files) == 0 {
		out = append(out, Finding{Severity: "blocker", Issue: "no changes were made"})
	}
	for _, f := range files {
		if isSensitive(f) {
			out = append(out, Finding{Severity: "blocker", File: f, Issue: "sensitive or CI file changed; the autopilot may not touch it"})
		} else if !InScope(f, scope) {
			out = append(out, Finding{Severity: "blocker", File: f, Issue: fmt.Sprintf("file is outside the agreed scope %v", scope)})
		}
	}
	for _, c := range checks {
		if !c.Passed {
			out = append(out, Finding{Severity: "blocker", Issue: fmt.Sprintf("check %q failed (exit %d): %s", c.Name, c.ExitCode, tail(c.Output, 600))})
		}
	}
	return out
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
