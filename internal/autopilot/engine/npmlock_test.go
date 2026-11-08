package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
)

var lockGenArgv = []string{"npm", "--prefix", "frontend", "install", "--package-lock-only", "--ignore-scripts"}

func TestLockGenArgvIsExact(t *testing.T) {
	for _, argv := range [][]string{
		{"npm", "install", "--package-lock-only", "--ignore-scripts"},
		{"npm", "--prefix", "frontend", "install", "--package-lock-only", "--ignore-scripts"},
		{"npm", "--prefix", "apps/web", "install", "--package-lock-only", "--ignore-scripts"},
	} {
		if _, ok := LockGenNPM(argv); !ok {
			t.Errorf("%v refused", argv)
		}
	}
	bad := [][]string{
		{"npm", "install", "--package-lock-only"},
		{"npm", "install", "--ignore-scripts"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "evil-package"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "axios@1"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--registry", "https://evil.example/"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--registry=https://evil.example/"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--before=2099-01-01"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--save"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--global"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--workspaces"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--force"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts", "--userconfig", "/tmp/x"},
		{"npm", "install", "--package-lock-only", "--ignore-scripts=false"},
		{"npm", "install", "--ignore-scripts", "--package-lock-only"},
		{"npm", "--ignore-scripts", "install", "--package-lock-only"},
		{"npm", "i", "--package-lock-only", "--ignore-scripts"},
		{"npm", "add", "--package-lock-only", "--ignore-scripts"},
		{"npm", "update", "--package-lock-only", "--ignore-scripts"},
		{"npm", "ci", "--package-lock-only", "--ignore-scripts"},
		{"npm", "--prefix", "/etc", "install", "--package-lock-only", "--ignore-scripts"},
		{"npm", "--prefix", "../..", "install", "--package-lock-only", "--ignore-scripts"},
		{"npm", "--prefix=frontend", "install", "--package-lock-only", "--ignore-scripts"},
		{"npm", "--prefix", "--registry", "install", "--package-lock-only", "--ignore-scripts"},
		{"/usr/bin/npm", "install", "--package-lock-only", "--ignore-scripts"},
		{"./npm", "install", "--package-lock-only", "--ignore-scripts"},
		{"yarn", "install", "--package-lock-only", "--ignore-scripts"},
		{"npx", "install", "--package-lock-only", "--ignore-scripts"},
		{},
	}
	for _, argv := range bad {
		if _, ok := LockGenNPM(argv); ok {
			t.Errorf("hostile argv accepted by LockGenNPM: %v", argv)
		}
	}
}

func genProfile(mod func(*Profile)) BoardConfig {
	p := Profile{
		Name: "core-frontend-toolchain", TitlePrefixes: []string{"[V2 FE TOOLCHAIN"},
		AllowLockfileGeneration: true,
		AllowedPaths:            []string{"frontend/package.json", "frontend/package-lock.json"},
		Checks: []Check{
			{Name: "lockfile", Argv: lockGenArgv, Network: true},
			{Name: "install", Argv: []string{"npm", "--prefix", "frontend", "ci", "--ignore-scripts"}, Network: true},
			{Name: "test", Argv: []string{"npm", "--prefix", "frontend", "run", "test"}},
		},
	}
	if mod != nil {
		mod(&p)
	}
	return boardWith([]Profile{p})
}

func TestLockfileGenerationIsOptInPerProfileAndFullyScoped(t *testing.T) {
	if err := validate(t, genProfile(nil)); err != nil {
		t.Fatalf("a correctly scoped profile was refused: %v", err)
	}
	cases := []struct {
		name string
		mod  func(*Profile)
		want string
	}{
		{"generation without the flag", func(p *Profile) { p.AllowLockfileGeneration = false }, "allowed only in a profile with allow_lockfile_generation"},
		{"flag without a generation check", func(p *Profile) { p.Checks = p.Checks[1:] }, "exactly one lockfile-generation check"},
		{"two generation checks", func(p *Profile) { p.Checks = append([]Check{p.Checks[0]}, p.Checks...) }, "exactly one lockfile-generation check"},
		{"generation without network", func(p *Profile) { p.Checks[0].Network = false }, "needs network:true"},
		{"profile does not own package.json", func(p *Profile) { p.AllowedPaths = []string{"frontend/package-lock.json"} }, "does not list \"frontend/package.json\""},
		{"profile does not own the lockfile", func(p *Profile) { p.AllowedPaths = []string{"frontend/package.json"} }, "does not list \"frontend/package-lock.json\""},
		{"profile owns a glob instead of the exact files", func(p *Profile) { p.AllowedPaths = []string{"frontend/**"} }, "does not list"},
		{"no allowed_paths at all", func(p *Profile) { p.AllowedPaths = nil }, "does not list"},
		{"lockfile also required before it exists", func(p *Profile) { p.RequireFiles = []string{"frontend/package-lock.json"} }, "generates it after that check runs"},
		{"no npm ci after generation", func(p *Profile) { p.Checks = []Check{p.Checks[0], p.Checks[2]} }, "no `npm ci --ignore-scripts` check"},
		{"npm ci placed before generation", func(p *Profile) { p.Checks = []Check{p.Checks[1], p.Checks[0], p.Checks[2]} }, "no `npm ci --ignore-scripts` check"},
		{"npm ci for another directory", func(p *Profile) { p.Checks[1].Argv = []string{"npm", "--prefix", "other", "ci", "--ignore-scripts"} }, "no `npm ci --ignore-scripts` check"},
		{"hostile flag on the generation argv", func(p *Profile) {
			p.Checks[0].Argv = append(append([]string{}, lockGenArgv...), "--registry=https://evil.example/")
		}, "network access"},
		{"a package name on the generation argv", func(p *Profile) { p.Checks[0].Argv = append(append([]string{}, lockGenArgv...), "left-pad") }, "network access"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validate(t, genProfile(tc.mod)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("config error = %v, want %q", err, tc.want)
			}
		})
	}
	// The capability exists nowhere else: not as a board-wide check, not in setup.
	legacy := boardWith(nil)
	legacy.Checks = []Check{{Name: "lockfile", Argv: lockGenArgv, Network: true}}
	if err := validate(t, legacy); err == nil || !strings.Contains(err.Error(), "allow_lockfile_generation") {
		t.Fatalf("board-wide lockfile generation accepted: %v", err)
	}
	setup := boardWith(nil)
	setup.Setup = []Check{{Name: "lockfile", Argv: lockGenArgv, Network: true}}
	if err := validate(t, setup); err == nil {
		t.Fatal("lockfile generation accepted as a setup command")
	}
	if err := ValidateCheck(Check{Name: "lockfile", Argv: lockGenArgv, Network: true}); err == nil {
		t.Fatal("ValidateCheck accepted generation outside a profile with the flag")
	}
}

func TestPackageJSONMayOnlyNameRegistryVersions(t *testing.T) {
	good := []string{
		`{"dependencies":{"axios":"1.21.0","@tanstack/react-router":"^1.170.46"},"devDependencies":{"vitest":"~5.1.0","jsdom":">=31 <32","x":"1.x","y":"1.2.3 - 2.0.0","z":"^1.2.3-rc.1"}}`,
		`{"name":"f","scripts":{"test":"vitest run"},"dependencies":{"zod":"4.6.7"},"overrides":{"foo":"1.2.3","bar":{"baz":"^2"},"qux@^1":"1.4.0"}}`,
		`{"name":"no-deps"}`,
	}
	for _, g := range good {
		if err := validatePackageJSON([]byte(g)); err != nil {
			t.Errorf("clean package.json refused: %v\n%s", err, g)
		}
	}
	bad := map[string]string{
		"git over https":      `{"dependencies":{"x":"git+https://github.com/o/r.git"}}`,
		"git over ssh":        `{"dependencies":{"x":"git+ssh://git@github.com/o/r.git#abc"}}`,
		"scp style git":       `{"dependencies":{"x":"git@github.com:o/r.git"}}`,
		"github shorthand":    `{"dependencies":{"x":"o/r"}}`,
		"github: prefix":      `{"dependencies":{"x":"github:o/r"}}`,
		"https tarball":       `{"dependencies":{"x":"https://evil.example/x.tgz"}}`,
		"http tarball":        `{"dependencies":{"x":"http://registry.npmjs.org/x/-/x-1.tgz"}}`,
		"file:":               `{"dependencies":{"x":"file:../x"}}`,
		"link:":               `{"dependencies":{"x":"link:../x"}}`,
		"workspace:":          `{"dependencies":{"x":"workspace:*"}}`,
		"npm alias":           `{"dependencies":{"x":"npm:other@1.0.0"}}`,
		"relative path":       `{"dependencies":{"x":"./local"}}`,
		"home path":           `{"dependencies":{"x":"~/x"}}`,
		"range hiding a url":  `{"dependencies":{"x":"1.0.0 || git+ssh://git@h/r"}}`,
		"dist-tag latest":     `{"dependencies":{"x":"latest"}}`,
		"dist-tag next":       `{"devDependencies":{"x":"next"}}`,
		"star":                `{"dependencies":{"x":"*"}}`,
		"empty spec":          `{"dependencies":{"x":""}}`,
		"spec too long":       `{"dependencies":{"x":"` + strings.Repeat("1", 80) + `"}}`,
		"uppercase name":      `{"dependencies":{"Left_Pad":"1.0.0"}}`,
		"path-like name":      `{"dependencies":{"../evil":"1.0.0"}}`,
		"scope traversal":     `{"dependencies":{"@scope/../x":"1.0.0"}}`,
		"url as name":         `{"dependencies":{"https://evil.example/x":"1.0.0"}}`,
		"peer git":            `{"peerDependencies":{"x":"git+https://h/r.git"}}`,
		"optional url":        `{"optionalDependencies":{"x":"https://evil.example/x.tgz"}}`,
		"override url":        `{"overrides":{"x":"https://evil.example/x.tgz"}}`,
		"nested override url": `{"overrides":{"a":{"b":"git+ssh://h/r"}}}`,
		"override key url":    `{"overrides":{"https://evil.example/x@1":"1.0.0"}}`,
		"override key range":  `{"overrides":{"foo@git+https://h/r":"1.0.0"}}`,
		"resolutions url":     `{"resolutions":{"x":"https://evil.example/x.tgz"}}`,
		"workspaces":          `{"workspaces":["packages/*"],"dependencies":{"x":"1.0.0"}}`,
		"spec not a string":   `{"dependencies":{"x":{"version":"1.0.0"}}}`,
		"deps not an object":  `{"dependencies":["x"]}`,
		"invalid json":        `{"dependencies":`,
		"not an object":       `["x"]`,
	}
	var many strings.Builder
	many.WriteString(`{"dependencies":{`)
	for i := 0; i < maxDeclaredDeps+1; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		fmt.Fprintf(&many, `"pkg-%d":"1.0.0"`, i)
	}
	many.WriteString("}}")
	bad["too many entries"] = many.String()
	bad["too large"] = `{"name":"` + strings.Repeat("a", maxPackageJSONBytes) + `"}`
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			if err := validatePackageJSON([]byte(body)); err == nil {
				t.Fatalf("accepted: %.200s", body)
			}
		})
	}
}

// ---- behaviour of the runner-owned step, with a fake npm so no network is needed ----

type genEnv struct {
	wt, bin, marker, beforeFile, counter string
}

func newGenEnv(t *testing.T, pkg string) *genEnv {
	t.Helper()
	e := &genEnv{wt: t.TempDir(), bin: t.TempDir()}
	e.marker = filepath.Join(e.bin, "ran")
	e.beforeFile = filepath.Join(e.bin, "before")
	e.counter = filepath.Join(e.bin, "counter")
	os.MkdirAll(filepath.Join(e.wt, "frontend"), 0o755)
	if err := os.WriteFile(filepath.Join(e.wt, "frontend", "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

const cleanPkg = `{"name":"f","scripts":{"test":"vitest run"},"devDependencies":{"vitest":"5.1.0","zod":"4.6.7"}}`

// fakeNPM installs an `npm` that insists on the exact argv, records that it ran,
// counts its invocations ($N) and then runs behaviour (a shell snippet).
func (e *genEnv) fakeNPM(t *testing.T, behaviour string) {
	t.Helper()
	script := `#!/bin/sh
touch ` + e.marker + `
[ "$*" = "--prefix frontend install --package-lock-only --ignore-scripts" ] || { echo "unexpected args: $*" >&2; exit 9; }
N=$(cat ` + e.counter + ` 2>/dev/null || echo 0); N=$((N+1)); echo $N > ` + e.counter + `
printf '%s' "$npm_config_before" > ` + e.beforeFile + `
LOCK=frontend/package-lock.json
` + behaviour + "\n"
	if err := os.WriteFile(filepath.Join(e.bin, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", e.bin+":"+os.Getenv("PATH"))
}

func (e *genEnv) run(t *testing.T, allow bool, ch Check, before time.Time) (res struct {
	Passed bool
	Output string
}, err error) {
	t.Helper()
	r, err := CmdChecker{Dir: e.wt, Unsandboxed: true, Checks: []Check{ch}, AllowLockGen: allow, Before: before}.Run(context.Background())
	if len(r) == 1 {
		res.Passed, res.Output = r[0].Passed, r[0].Output
	}
	return res, err
}

func (e *genEnv) ran() bool { _, err := os.Stat(e.marker); return err == nil }

var genCheck = Check{Name: "lockfile", Argv: lockGenArgv, Network: true}

func writeGoodLock() string {
	return "cat > $LOCK <<'EOF'\n" + lockfile(goodEntry) + "\nEOF\n"
}

func TestLockGenRunsOnlyTheRunnerOwnedStepAndKeepsTheLockfileHonest(t *testing.T) {
	before := time.Date(2026, 11, 8, 9, 24, 38, 0, time.UTC)
	t.Run("a clean package.json produces a validated, stable lockfile", func(t *testing.T) {
		e := newGenEnv(t, cleanPkg)
		// An agent-written lockfile must never be trusted: the fake fails if it
		// is still there on the first pass.
		os.WriteFile(filepath.Join(e.wt, "frontend", "package-lock.json"), []byte(`{"AGENT":"forged"}`), 0o644)
		e.fakeNPM(t, `[ "$N" = 1 ] && [ -e $LOCK ] && { echo "a stale lockfile was still present" >&2; exit 8; }
[ -e $LOCK ] || {
`+writeGoodLock()+`}`)
		res, err := e.run(t, true, genCheck, before)
		if err != nil || !res.Passed {
			t.Fatalf("passed=%v err=%v\n%s", res.Passed, err, res.Output)
		}
		for _, want := range []string{"--before=2026-11-08T09:24:38.000Z", "pass 1: package-lock.json sha256=", "byte-identical"} {
			if !strings.Contains(res.Output, want) {
				t.Errorf("output lacks %q:\n%s", want, res.Output)
			}
		}
		got, _ := os.ReadFile(filepath.Join(e.wt, "frontend", "package-lock.json"))
		if strings.Contains(string(got), "AGENT") || !strings.Contains(string(got), `"lockfileVersion":3`) {
			t.Fatalf("the agent's lockfile survived or the generated one is wrong: %s", got)
		}
		if b, _ := os.ReadFile(e.beforeFile); string(b) != "2026-11-08T09:24:38.000Z" {
			t.Fatalf("npm_config_before = %q", b)
		}
		if n, _ := os.ReadFile(e.counter); strings.TrimSpace(string(n)) != "2" {
			t.Fatalf("npm ran %q times, want exactly 2 (generate, then confirm stability)", n)
		}
	})
	t.Run("a node_modules left by an earlier round is cleared first", func(t *testing.T) {
		e := newGenEnv(t, cleanPkg)
		nm := filepath.Join(e.wt, "frontend", "node_modules", "left-over")
		os.MkdirAll(nm, 0o755)
		os.WriteFile(filepath.Join(nm, "package.json"), []byte(`{"name":"left-over"}`), 0o644)
		os.WriteFile(filepath.Join(e.wt, "frontend", "node_modules", ".package-lock.json"), []byte(`{}`), 0o644)
		// The fake fails on the first pass if node_modules is still there, which
		// is exactly the condition that makes real npm drop "resolved" fields.
		e.fakeNPM(t, `[ "$N" = 1 ] && [ -e frontend/node_modules ] && { echo "node_modules was still present" >&2; exit 8; }
[ -e $LOCK ] || {
`+writeGoodLock()+`}`)
		if res, err := e.run(t, true, genCheck, before); err != nil || !res.Passed {
			t.Fatalf("passed=%v err=%v\n%s", res.Passed, err, res.Output)
		}
		if _, err := os.Stat(filepath.Join(e.wt, "frontend", "node_modules")); err == nil {
			t.Fatal("node_modules survived generation")
		}
	})
	t.Run("with no Before the run still passes a valid timestamp", func(t *testing.T) {
		e := newGenEnv(t, cleanPkg)
		e.fakeNPM(t, `[ -e $LOCK ] || {
`+writeGoodLock()+`}`)
		if res, err := e.run(t, true, genCheck, time.Time{}); err != nil || !res.Passed {
			t.Fatalf("%v %v %s", res.Passed, err, res.Output)
		}
		b, _ := os.ReadFile(e.beforeFile)
		if !beforeStamp.MatchString(string(b)) {
			t.Fatalf("npm_config_before = %q is not an RFC 3339 UTC timestamp", b)
		}
	})
	failures := map[string]struct{ behaviour, want string }{
		"npm fails":                           {`echo "npm ERR! code ETARGET: no matching version" >&2; exit 1`, "ETARGET"},
		"npm succeeds but writes no lockfile": {`:`, "not produced"},
		"lockfile resolves off-registry":      {"cat > $LOCK <<'EOF'\n" + lockfile(`,"node_modules/x":{"resolved":"https://evil.example/x.tgz","integrity":"sha512-A"}`) + "\nEOF", "resolves from"},
		"lockfile with a git source":          {"cat > $LOCK <<'EOF'\n" + lockfile(`,"node_modules/x":{"resolved":"git+ssh://git@github.com/o/r.git#abc","integrity":"sha512-A"}`) + "\nEOF", "resolves from"},
		"lockfile without integrity":          {"cat > $LOCK <<'EOF'\n" + lockfile(`,"node_modules/x":{"resolved":"https://registry.npmjs.org/x/-/x-1.tgz"}`) + "\nEOF", "integrity"},
		"lockfile with a linked package":      {"cat > $LOCK <<'EOF'\n" + lockfile(`,"node_modules/x":{"resolved":"../x","link":true}`) + "\nEOF", "link"},
		"lockfileVersion 4":                   {`printf '{"lockfileVersion":4,"packages":{"":{},"node_modules/zod":{"resolved":"https://registry.npmjs.org/zod/-/zod-1.tgz","integrity":"sha512-A"}}}' > $LOCK`, "lockfileVersion 2 or 3"},
		"lockfileVersion 1":                   {`printf '{"lockfileVersion":1,"dependencies":{}}' > $LOCK`, "lockfileVersion 2 or 3"},
		"empty lockfile":                      {`: > $LOCK`, "empty"},
		"lockfile larger than the size cap":   {`head -c 9000000 /dev/zero | tr "\\0" "a" > $LOCK`, "larger than"},
		"lockfile is not JSON":                {`echo "not json" > $LOCK`, "not valid JSON"},
		"lockfile is a symlink":               {`ln -s /etc/hostname $LOCK`, "regular file"},
		"lockfile is a directory":             {`mkdir $LOCK`, "regular file"},
		"npm edits package.json": {`[ -e $LOCK ] || {` + "\n" + writeGoodLock() + `}
echo " " >> frontend/package.json`, "changed package.json"},
		"lockfile changes on the second pass": {`if [ "$N" = 1 ]; then
` + writeGoodLock() + `else
cat > $LOCK <<'EOF'
` + lockfile(goodEntry+`,"node_modules/extra":{"resolved":"https://registry.npmjs.org/extra/-/extra-1.tgz","integrity":"sha512-B"}`) + `
EOF
fi`, "not stable"},
		"second pass fails": {`if [ "$N" = 1 ]; then
` + writeGoodLock() + `else
echo "registry unavailable" >&2; exit 1
fi`, "registry unavailable"},
	}
	for name, f := range failures {
		t.Run(name, func(t *testing.T) {
			e := newGenEnv(t, cleanPkg)
			e.fakeNPM(t, f.behaviour)
			res, err := e.run(t, true, genCheck, before)
			if err != nil {
				t.Fatal(err)
			}
			if res.Passed {
				t.Fatalf("a bad generation counted as PASS:\n%s", res.Output)
			}
			if !strings.Contains(res.Output, f.want) {
				t.Fatalf("output does not say why (%q):\n%s", f.want, res.Output)
			}
		})
	}
}

func TestLockGenIsRefusedBeforeNPMEverRuns(t *testing.T) {
	bad := func(pkg string) string { return pkg }
	cases := map[string]func(*genEnv, *testing.T){
		"git dependency": func(e *genEnv, t *testing.T) {
			os.WriteFile(filepath.Join(e.wt, "frontend", "package.json"), []byte(bad(`{"dependencies":{"x":"git+https://github.com/o/r.git"}}`)), 0o644)
		},
		"tarball dependency": func(e *genEnv, t *testing.T) {
			os.WriteFile(filepath.Join(e.wt, "frontend", "package.json"), []byte(`{"dependencies":{"x":"https://evil.example/x.tgz"}}`), 0o644)
		},
		"file dependency": func(e *genEnv, t *testing.T) {
			os.WriteFile(filepath.Join(e.wt, "frontend", "package.json"), []byte(`{"dependencies":{"x":"file:../../etc"}}`), 0o644)
		},
		"project .npmrc": func(e *genEnv, t *testing.T) {
			os.WriteFile(filepath.Join(e.wt, "frontend", ".npmrc"), []byte("@scope:registry=https://evil.example/\nignore-scripts=false\n"), 0o644)
		},
		"root .npmrc": func(e *genEnv, t *testing.T) {
			os.WriteFile(filepath.Join(e.wt, ".npmrc"), []byte("registry=https://evil.example/\n"), 0o644)
		},
		"npm-shrinkwrap.json": func(e *genEnv, t *testing.T) {
			os.WriteFile(filepath.Join(e.wt, "frontend", "npm-shrinkwrap.json"), []byte(lockfile(goodEntry)), 0o644)
		},
		"package.json is a symlink out of the worktree": func(e *genEnv, t *testing.T) {
			out := t.TempDir()
			os.WriteFile(filepath.Join(out, "package.json"), []byte(cleanPkg), 0o644)
			os.Remove(filepath.Join(e.wt, "frontend", "package.json"))
			os.Symlink(filepath.Join(out, "package.json"), filepath.Join(e.wt, "frontend", "package.json"))
		},
		"project directory is a symlink out of the worktree": func(e *genEnv, t *testing.T) {
			out := t.TempDir()
			os.WriteFile(filepath.Join(out, "package.json"), []byte(cleanPkg), 0o644)
			os.RemoveAll(filepath.Join(e.wt, "frontend"))
			os.Symlink(out, filepath.Join(e.wt, "frontend"))
		},
		"node_modules is a symlink": func(e *genEnv, t *testing.T) {
			out := t.TempDir()
			os.WriteFile(filepath.Join(out, "keep"), []byte("must survive"), 0o644)
			os.Symlink(out, filepath.Join(e.wt, "frontend", "node_modules"))
			t.Cleanup(func() {
				if b, err := os.ReadFile(filepath.Join(out, "keep")); err != nil || string(b) != "must survive" {
					t.Errorf("the symlink target was damaged: %v %q", err, b)
				}
			})
		},
		"package.json is missing": func(e *genEnv, t *testing.T) { os.Remove(filepath.Join(e.wt, "frontend", "package.json")) },
		"package.json is a directory": func(e *genEnv, t *testing.T) {
			os.Remove(filepath.Join(e.wt, "frontend", "package.json"))
			os.Mkdir(filepath.Join(e.wt, "frontend", "package.json"), 0o755)
		},
		"existing lockfile is a symlink": func(e *genEnv, t *testing.T) {
			os.Symlink("/etc/hostname", filepath.Join(e.wt, "frontend", "package-lock.json"))
		},
		"workspaces": func(e *genEnv, t *testing.T) {
			os.WriteFile(filepath.Join(e.wt, "frontend", "package.json"), []byte(`{"workspaces":["../*"],"dependencies":{"x":"1.0.0"}}`), 0o644)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newGenEnv(t, cleanPkg)
			e.fakeNPM(t, `:`)
			setup(e, t)
			res, err := e.run(t, true, genCheck, time.Now())
			if err != nil || res.Passed || !strings.Contains(res.Output, "refused") {
				t.Fatalf("passed=%v err=%v\n%s", res.Passed, err, res.Output)
			}
			if e.ran() {
				t.Fatal("npm was started although the preconditions failed")
			}
		})
	}
	t.Run("the profile did not enable it", func(t *testing.T) {
		e := newGenEnv(t, cleanPkg)
		e.fakeNPM(t, writeGoodLock())
		res, err := e.run(t, false, genCheck, time.Now())
		if err != nil || res.Passed || !strings.Contains(res.Output, "does not enable") || e.ran() {
			t.Fatalf("passed=%v err=%v ran=%v\n%s", res.Passed, err, e.ran(), res.Output)
		}
	})
	t.Run("the check has no network flag", func(t *testing.T) {
		e := newGenEnv(t, cleanPkg)
		e.fakeNPM(t, writeGoodLock())
		res, err := e.run(t, true, Check{Name: "lockfile", Argv: lockGenArgv}, time.Now())
		if err != nil || res.Passed || e.ran() {
			t.Fatalf("passed=%v err=%v ran=%v\n%s", res.Passed, err, e.ran(), res.Output)
		}
	})
	t.Run("a hostile argv is not treated as the generation step", func(t *testing.T) {
		e := newGenEnv(t, cleanPkg)
		e.fakeNPM(t, writeGoodLock())
		ch := Check{Name: "lockfile", Argv: append(append([]string{}, lockGenArgv...), "--registry=https://evil.example/"), Network: true}
		res, err := e.run(t, true, ch, time.Now())
		if err != nil || res.Passed || !strings.Contains(res.Output, "refused before running") || e.ran() {
			t.Fatalf("passed=%v err=%v ran=%v\n%s", res.Passed, err, e.ran(), res.Output)
		}
	})
	t.Run("no sandbox and not a test run is an infrastructure error, never a run", func(t *testing.T) {
		e := newGenEnv(t, cleanPkg)
		e.fakeNPM(t, writeGoodLock())
		_, err := CmdChecker{Dir: e.wt, Checks: []Check{genCheck}, AllowLockGen: true}.Run(context.Background())
		if err == nil || e.ran() {
			t.Fatalf("err=%v ran=%v", err, e.ran())
		}
	})
}

func TestSandboxPassesOnlyAWellFormedBeforeDate(t *testing.T) {
	wt := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	ok, err := Sandbox{Worktree: wt, Bwrap: "/usr/bin/bwrap", Network: true, NPMBefore: "2026-11-08T09:24:38.000Z"}.Wrap([]string{"npm"})
	if err != nil {
		t.Skipf("sandbox wrap: %v", err)
	}
	if !contains(ok, "npm_config_before") || !contains(ok, "2026-11-08T09:24:38.000Z") {
		t.Fatalf("the before date was not passed: %v", ok)
	}
	for _, v := range []string{"yesterday", "2026-11-08", "2026-11-08T09:24:38+07:00", "2026-11-08T09:24:38.000Z --registry=x", "1; rm -rf /", ""} {
		if v == "" {
			continue
		}
		if _, err := (Sandbox{Worktree: wt, Bwrap: "/usr/bin/bwrap", Network: true, NPMBefore: v}).Wrap([]string{"npm"}); err == nil {
			t.Errorf("a malformed before value was accepted: %q", v)
		}
	}
	off, _ := Sandbox{Worktree: wt, Bwrap: "/usr/bin/bwrap", NPMBefore: "2026-11-08T09:24:38.000Z"}.Wrap([]string{"npm"})
	if contains(off, "npm_config_before") || !contains(off, "--unshare-net") {
		t.Fatalf("without Network the sandbox must stay offline and carry no npm environment: %v", off)
	}
}

func TestTheAgentIsToldTheRunnerOwnsTheLockfile(t *testing.T) {
	bc := genProfile(nil)
	r := Resolved{BC: bc.Profiles[0].Resolved(bc)}
	r.BC.Profiles = bc.Profiles
	cc := cardContext(board.Card{Key: "BII-898", Title: "[V2 FE TOOLCHAIN] Pinned FE toolchain"}, r)
	var found bool
	for _, line := range cc.Checks {
		if strings.Contains(line, "install --package-lock-only") {
			found = strings.Contains(line, "run by the autopilot, not by you") && strings.Contains(line, "do not write or edit package-lock.json")
		}
	}
	if !found {
		t.Fatalf("the agent is not told who generates the lockfile: %v", cc.Checks)
	}
	for _, line := range cc.Checks {
		if strings.Contains(line, " ci --ignore-scripts") && strings.Contains(line, "run by the autopilot") {
			t.Fatalf("only the generation step should carry the note: %q", line)
		}
	}
}

// ---- real npm, real bubblewrap, real registry: opt-in, needs network ----

// THEFLOOR_NPM_E2E=1 THEFLOOR_NODE_DIR=<nvm node dir> go test -run TestRealNPMLockfileGeneration
func TestRealNPMLockfileGeneration(t *testing.T) {
	nodeDir := os.Getenv("THEFLOOR_NODE_DIR")
	if os.Getenv("THEFLOOR_NPM_E2E") != "1" || nodeDir == "" {
		t.Skip("set THEFLOOR_NPM_E2E=1 and THEFLOOR_NODE_DIR to run against the real registry")
	}
	needBwrap(t)
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not on PATH")
	}
	wt := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(wt, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("frontend/package.json", `{"name":"fixture","private":true,"type":"module","scripts":{"test":"vitest run"},
 "devDependencies":{"vitest":"5.1.0","zod":"4.6.7","oxlint":"1.92.0"}}`)
	write("frontend/tests/smoke.test.ts", `import { test, expect } from "vitest";
import { z } from "zod";
test("zod accepts a valid value", () => expect(z.string().min(3).parse("abc")).toBe("abc"));
test("zod rejects an invalid value", () => expect(() => z.string().min(3).parse("a")).toThrow());
test("arithmetic still works", () => expect(1 + 1).toBe(2));
`)
	checks := []Check{
		genCheck,
		{Name: "install", Argv: []string{"npm", "--prefix", "frontend", "ci", "--ignore-scripts"}, Network: true},
		{Name: "test", Argv: []string{"npm", "--prefix", "frontend", "run", "test"},
			RequireOutput: `(?m)^\s*Tests\s+[1-9][0-9]* passed`, RejectOutput: `(?m)^\s*Tests\s.*\b(skipped|todo|failed)\b`},
		{Name: "lint", Argv: []string{"frontend/node_modules/.bin/oxlint", "frontend/tests"}},
	}
	res, err := CmdChecker{Dir: wt, Sandbox: &Sandbox{Worktree: wt, ReadOnly: []string{nodeDir}}, Checks: checks,
		AllowLockGen: true, Before: time.Now(), Timeout: 8 * time.Minute}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if !r.Passed {
			t.Fatalf("[%s] failed (exit %d): %s\n%s", r.Name, r.ExitCode, r.Note, r.Output)
		}
		t.Logf("[%s] PASS", r.Name)
	}
	lock, err := os.ReadFile(filepath.Join(wt, "frontend", "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		LockfileVersion int                        `json:"lockfileVersion"`
		Packages        map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(lock, &doc); err != nil || doc.LockfileVersion != 3 || len(doc.Packages) < 20 {
		t.Fatalf("lockfile version %d with %d entries (%v)", doc.LockfileVersion, len(doc.Packages), err)
	}
	for _, m := range regexp.MustCompile(`"resolved":\s*"([^"]*)"`).FindAllStringSubmatch(string(lock), -1) {
		if !strings.HasPrefix(m[1], AllowedNPMRegistry) {
			t.Fatalf("the generated lockfile resolves off-registry: %s", m[1])
		}
	}
	t.Logf("lockfileVersion %d, %d entries, every resolved URL on registry.npmjs.org", doc.LockfileVersion, len(doc.Packages))

	// Hostile package.json against the same real runner: refused before any network.
	for name, body := range map[string]string{
		"git dependency":     `{"name":"f","dependencies":{"x":"git+https://github.com/o/r.git"}}`,
		"tarball dependency": `{"name":"f","dependencies":{"x":"https://evil.example/x.tgz"}}`,
		"file dependency":    `{"name":"f","dependencies":{"x":"file:../../etc"}}`,
		"npm alias":          `{"name":"f","dependencies":{"x":"npm:other@1.0.0"}}`,
	} {
		write("frontend/package.json", body)
		r, err := CmdChecker{Dir: wt, Sandbox: &Sandbox{Worktree: wt, ReadOnly: []string{nodeDir}}, Checks: []Check{genCheck}, AllowLockGen: true, Before: time.Now()}.Run(context.Background())
		if err != nil || len(r) != 1 || r[0].Passed || !strings.Contains(r[0].Output, "refused") {
			t.Fatalf("%s was not refused: %+v %v", name, r, err)
		}
		t.Logf("refused: %s", name)
	}
}
