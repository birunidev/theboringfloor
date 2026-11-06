package ocv2

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// SpaceServer is the only Space MCP server name the autopilot accepts; its
// tools reach opencode as "birunidev-space_<tool>".
const SpaceServer = "birunidev-space"

// Policy is the autopilot's code-level allowlist for one session. It fails
// closed: an action it has no rule for is rejected.
type Policy struct {
	// Root is the card worktree. Path resources must resolve inside it.
	Root string
	// ReadActions may read paths inside Root (e.g. read, glob, grep, list).
	ReadActions []string
	// WriteActions may modify paths inside Root (e.g. edit, write, patch).
	// Empty for read-only stages. Writes into .git are always refused.
	WriteActions []string
	// BashAction is the action name of shell commands (e.g. "bash").
	BashAction string
	// Bash holds allowed command prefixes, whole words: "go test",
	// "npm run lint". A command must start with one of them.
	Bash []string
	// Space gates birunidev-space MCP tools; nil rejects all of them.
	Space *SpaceGate
}

const shellMeta = ";&|`$<>\n\r\\"

// Decide implements Approver.
func (p *Policy) Decide(r PermissionRequest) Decision {
	a := r.Action
	switch {
	case a == "opencode_list_mcp_resources":
		if p.Space != nil {
			return allow("list MCP resources (metadata only)")
		}
		return reject("MCP access not allowed in this stage")
	case strings.HasPrefix(a, SpaceServer+"_"):
		if p.Space == nil {
			return reject("Space tools not allowed in this stage")
		}
		return p.Space.Decide(strings.TrimPrefix(a, SpaceServer+"_"), r.Input())
	case strings.Contains(a, "_") && !contains(p.ReadActions, a) && !contains(p.WriteActions, a) && a != p.BashAction:
		// Another MCP server's tool (github_*, figma-mcp_*, ...).
		return reject("MCP server not allowed: " + a)
	case p.BashAction != "" && a == p.BashAction:
		return p.decideBash(r.Resources)
	case contains(p.WriteActions, a):
		return p.decidePaths(r.Resources, true)
	case contains(p.ReadActions, a):
		return p.decidePaths(r.Resources, false)
	}
	return reject("not on allowlist: " + a)
}

func (p *Policy) decidePaths(resources []string, write bool) Decision {
	if len(resources) == 0 {
		return reject("path action without a path")
	}
	for _, res := range resources {
		if err := p.inside(res, write); err != nil {
			return reject(err.Error())
		}
	}
	if write {
		return allow("write inside worktree")
	}
	return allow("read inside worktree")
}

// inside resolves res against Root (following symlinks of the existing
// part of the path) and refuses anything that lands outside it.
func (p *Policy) inside(res string, write bool) error {
	if p.Root == "" {
		return errors.New("no worktree root configured")
	}
	if res == "" || res == "*" || strings.ContainsAny(res, "*?[") {
		return fmt.Errorf("refusing wildcard path %q", res)
	}
	root, err := filepath.EvalSymlinks(p.Root)
	if err != nil {
		return fmt.Errorf("worktree root: %w", err)
	}
	path := res
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.Root, path)
	}
	path = filepath.Clean(path)
	// Resolve symlinks on the longest existing prefix.
	resolved, rest := path, ""
	for {
		if r, err := filepath.EvalSymlinks(resolved); err == nil {
			resolved = filepath.Join(r, rest)
			break
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			return fmt.Errorf("cannot resolve %q", res)
		}
		rest = filepath.Join(filepath.Base(resolved), rest)
		resolved = parent
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("path %q is outside the worktree", res)
	}
	if write && (rel == ".git" || strings.HasPrefix(rel, ".git"+string(os.PathSeparator))) {
		return fmt.Errorf("refusing write into .git: %q", res)
	}
	return nil
}

func (p *Policy) decideBash(resources []string) Decision {
	if len(resources) == 0 {
		return reject("shell request without a command")
	}
	for _, cmd := range resources {
		c := strings.TrimSpace(cmd)
		if c == "" || strings.ContainsAny(c, shellMeta) {
			return reject(fmt.Sprintf("shell metacharacters not allowed: %q", trim(c, 120)))
		}
		words := strings.Fields(c)
		ok := false
		for _, prefix := range p.Bash {
			pw := strings.Fields(prefix)
			if len(pw) > 0 && len(words) >= len(pw) && reflect.DeepEqual(words[:len(pw)], pw) {
				ok = true
				break
			}
		}
		if !ok {
			return reject(fmt.Sprintf("command not on allowlist: %q", trim(c, 120)))
		}
		for _, w := range words {
			// A ".." path segment climbs out of the worktree; Go's "./..."
			// package pattern has a "..." segment and is fine.
			for _, seg := range strings.FieldsFunc(w, func(r rune) bool { return r == '/' || r == '=' }) {
				if seg == ".." {
					return reject(fmt.Sprintf("parent-directory path in command: %q", trim(c, 120)))
				}
			}
			if filepath.IsAbs(w) {
				return reject(fmt.Sprintf("absolute path in command: %q", trim(c, 120)))
			}
		}
	}
	return allow("allowlisted command")
}

// ExpectedWrite is one Space write the autopilot planned. Args must equal
// the tool input exactly; only idempotency_key and workspace_id may differ.
type ExpectedWrite struct {
	Tool string
	Args map[string]any
}

// SpaceGate approves Space reads and only pre-registered Space writes.
type SpaceGate struct {
	mu       sync.Mutex
	reads    map[string]bool
	expected []ExpectedWrite
	// Forbidden list ids no move_card may target, even if expected.
	forbiddenLists map[string]bool
}

// DefaultSpaceReads are the read-only Space tools stages may call.
var DefaultSpaceReads = []string{
	"get_card", "list_cards", "get_board", "list_boards",
	"search_context", "get_document", "list_documents", "get_document_links",
	"get_brand_context",
}

// NewSpaceGate allows reads and refuses moves into forbiddenListIDs.
func NewSpaceGate(reads []string, forbiddenListIDs ...string) *SpaceGate {
	g := &SpaceGate{reads: map[string]bool{}, forbiddenLists: map[string]bool{}}
	for _, r := range reads {
		g.reads[r] = true
	}
	for _, id := range forbiddenListIDs {
		if id = strings.TrimSpace(id); id != "" {
			g.forbiddenLists[id] = true
		}
	}
	return g
}

// Expect registers one planned write. It is consumed when approved.
func (g *SpaceGate) Expect(w ExpectedWrite) error {
	if w.Tool == "move_card" {
		if l, _ := w.Args["list_id"].(string); g.forbiddenLists[strings.TrimSpace(l)] {
			return fmt.Errorf("ocv2: refusing to expect a move into forbidden list %q", l)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.expected = append(g.expected, w)
	return nil
}

// Pending returns planned writes not yet consumed.
func (g *SpaceGate) Pending() []ExpectedWrite {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]ExpectedWrite(nil), g.expected...)
}

var ignoredWriteKeys = map[string]bool{"idempotency_key": true, "workspace_id": true}

// Decide approves a Space tool call.
func (g *SpaceGate) Decide(tool string, input map[string]any) Decision {
	if g.reads[tool] {
		return allow("Space read: " + tool)
	}
	if tool == "move_card" {
		if l, _ := input["list_id"].(string); g.forbiddenLists[strings.TrimSpace(l)] {
			return reject("move into a forbidden list (Done) is never allowed")
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, w := range g.expected {
		if w.Tool == tool && argsMatch(w.Args, input) {
			g.expected = append(g.expected[:i], g.expected[i+1:]...)
			return allow("planned Space write: " + tool)
		}
	}
	return reject("unplanned Space write: " + tool)
}

func argsMatch(want, got map[string]any) bool {
	n := 0
	for k, v := range got {
		if ignoredWriteKeys[k] {
			continue
		}
		wv, ok := want[k]
		if !ok || fmt.Sprint(wv) != fmt.Sprint(v) {
			return false
		}
		n++
	}
	return n == len(want)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func allow(r string) Decision  { return Decision{Allow: true, Reason: r} }
func reject(r string) Decision { return Decision{Allow: false, Reason: r} }
