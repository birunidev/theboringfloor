package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The BII-1034 profile in the docs is what the owner will apply, so it is parsed, validated and exercised here
// instead of trusted.
func TestShadcnAtomsProfileInTheDocsIsValidAndStrict(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "autopilot-bii-1034-shadcn-atoms-profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	start := strings.Index(text, "```json\n")
	end := strings.Index(text[start+8:], "```")
	if start < 0 || end < 0 {
		t.Fatal("no json block in the doc")
	}
	var p Profile
	if err := json.Unmarshal([]byte(text[start+8:start+8+end]), &p); err != nil {
		t.Fatalf("the doc's profile does not parse: %v", err)
	}
	// The v2 board also carries the other profiles; the new one must be valid alongside a neighbour with a
	// different prefix, so build the board the way the config does.
	other := Profile{Name: "core-frontend-tailwind", TitlePrefixes: []string{"[V2 FE TAILWIND"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}}
	if err := validate(t, boardWith([]Profile{other, p})); err != nil {
		t.Fatalf("the documented profile is refused by config validation: %v", err)
	}
	bc := boardWith([]Profile{other, p})
	got, err := bc.ProfileFor("[V2 FE SHADCN-ATOMS] Clean-room shadcn Badge Button Card Input primitives")
	if err != nil || got.Name != "core-frontend-shadcn-atoms" {
		t.Fatalf("the BII-900 title routes to %q (%v)", got.Name, err)
	}
	if _, err := bc.ProfileFor("[V2 FE SHADCN-ATOMS Missing bracket]"); !errors.Is(err, ErrNoProfile) {
		t.Errorf("a prefix without its closing bracket must be refused, got %v", err)
	}
	if _, err := bc.ProfileFor("[V2 FE ROUTES] something new"); !errors.Is(err, ErrNoProfile) {
		t.Errorf("an undefined frontend slice must be refused, got %v", err)
	}

	want := []string{
		"frontend/src/components/shadcn/ui/badge.tsx", "frontend/src/components/shadcn/ui/button.tsx",
		"frontend/src/components/shadcn/ui/card.tsx", "frontend/src/components/shadcn/ui/input.tsx",
		"frontend/tests/shadcn-atoms.test.tsx",
	}
	if err := p.Owns(want); err != nil {
		t.Fatalf("the five files must be owned: %v", err)
	}
	if len(p.AllowedPaths) != len(want) {
		t.Errorf("the profile owns %d paths, want exactly %d", len(p.AllowedPaths), len(want))
	}
	for _, bad := range []string{
		"frontend/package.json", "frontend/package-lock.json", "frontend/vitest.config.ts", "frontend/vite.config.ts",
		"frontend/tests/setup.ts", "frontend/src/lib/utils.ts", "frontend/src/styles/app.css",
		"frontend/tests/toolchain-smoke.test.ts", "frontend/src/components/shadcn/ui/dialog.tsx", "backend/app/main.py",
	} {
		if err := p.Owns(append([]string{bad}, want...)); err == nil {
			t.Errorf("%s must not be owned by the profile", bad)
		}
	}

	if p.AllowLockfileGeneration {
		t.Error("this profile must not generate a lockfile; the lockfile already exists and is not owned")
	}
	for _, ch := range p.Checks {
		joined := strings.Join(ch.Argv, " ")
		if strings.Contains(joined, "run build") || strings.Contains(joined, "run typecheck") {
			t.Errorf("check %q runs the global build/typecheck, which fails at the base: %s", ch.Name, joined)
		}
		if ch.Network {
			if _, ok := LockedNPMInstall(ch.Argv); !ok {
				t.Errorf("check %q has network access but is not the strict npm ci: %s", ch.Name, joined)
			}
		}
	}
	var gate Check
	for _, ch := range p.Checks {
		if ch.Name == "shadcn atoms test" {
			gate = ch
		}
	}
	if gate.RequireOutput == "" || gate.RejectOutput == "" {
		t.Fatal("the atoms test must require a passing count and reject skipped, todo and failed")
	}
}
