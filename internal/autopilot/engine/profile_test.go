package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
)

func testProfiles() []Profile {
	return []Profile{
		{Name: "core-frontend", TitlePrefixes: []string{"[V2 FE"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}},
		{Name: "core-backend", TitlePrefixes: []string{"[V2 BE"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}},
	}
}

func boardWith(profiles []Profile) BoardConfig {
	return BoardConfig{Name: "V2 Engineering", BoardID: "b", RepoName: "r", RepoDir: "/x", ReadyList: "Autopilot Queue", Label: "none", Profiles: profiles}
}

func validate(t *testing.T, bc BoardConfig) error {
	t.Helper()
	c := Config{Boards: []BoardConfig{bc}, PerCardTokens: 40000, DailyTokens: 1000000}
	c.Defaults()
	return c.Validate()
}

func TestProfileSelectionIsExplicitAndFailsClosed(t *testing.T) {
	bc := boardWith(testProfiles())
	if p, err := bc.ProfileFor("[V2 FE TOOLCHAIN] pin deps"); err != nil || p.Name != "core-frontend" {
		t.Fatalf("frontend card: %v %v", p.Name, err)
	}
	if p, err := bc.ProfileFor("  [v2 be QA] negative test "); err != nil || p.Name != "core-backend" {
		t.Fatalf("backend card (case/space): %v %v", p.Name, err)
	}
	if _, err := bc.ProfileFor("fix the thing"); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("card without a matching profile: %v", err)
	}
	if _, err := bc.ProfileFor(""); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("empty title: %v", err)
	}
	amb := boardWith([]Profile{
		{Name: "a", TitlePrefixes: []string{"[V2"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}},
		{Name: "b", TitlePrefixes: []string{"[V2 FE"}, Checks: []Check{{Name: "ok", Argv: []string{"true"}}}},
	})
	if _, err := amb.ProfileFor("[V2 FE X]"); !errors.Is(err, ErrAmbiguousProfile) {
		t.Fatalf("conflicting profiles: %v", err)
	}
	// Boards without profiles keep the legacy board-wide checks.
	legacy := BoardConfig{Checks: []Check{{Name: "legacy", Argv: []string{"true"}}}}
	if p, err := legacy.ProfileFor("anything"); err != nil || p.Name != "board-default" || len(p.Checks) != 1 {
		t.Fatalf("legacy board: %+v %v", p, err)
	}
}

func TestProfileConfigRefusals(t *testing.T) {
	ok := []Check{{Name: "ok", Argv: []string{"true"}}}
	cases := []struct {
		name string
		bc   func() BoardConfig
		want string
	}{
		{"overlapping prefixes", func() BoardConfig {
			return boardWith([]Profile{{Name: "a", TitlePrefixes: []string{"[V2"}, Checks: ok}, {Name: "b", TitlePrefixes: []string{"[V2 FE"}, Checks: ok}})
		}, "overlap"},
		{"board checks and profiles together", func() BoardConfig {
			b := boardWith(testProfiles())
			b.Checks = ok
			return b
		}, "both board-wide checks and profiles"},
		{"profile with no checks", func() BoardConfig {
			return boardWith([]Profile{{Name: "empty", TitlePrefixes: []string{"[V2"}}})
		}, "no checks"},
		{"profile without prefixes", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", Checks: ok}})
		}, "title_prefixes"},
		{"duplicate name", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: ok}, {Name: "x", TitlePrefixes: []string{"[B"}, Checks: ok}})
		}, "duplicate profile name"},
		{"shell", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "s", Argv: []string{"sh", "-c", "npm test"}}}}})
		}, "forbidden command"},
		{"curl", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "s", Argv: []string{"/usr/bin/curl", "http://x"}}}}})
		}, "forbidden command"},
		{"npx", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "s", Argv: []string{"npx", "vitest"}}}}})
		}, "forbidden command"},
		{"npm exec", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "s", Argv: []string{"npm", "exec", "evil"}}}}})
		}, "downloaded code"},
		{"python inline code", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "s", Argv: []string{"python", "-c", "import os"}}}}})
		}, "inline code"},
		{"python inline code hidden behind a value flag", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "s", Argv: []string{"python3.12", "-W", "ignore", "-c", "import os"}}}}})
		}, "inline code"},
		{"node eval", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "s", Argv: []string{"node", "--eval", "1"}}}}})
		}, "inline code"},
		{"pytest accepting exit 5", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "t", Argv: []string{"/venv/bin/python", "-m", "pytest", "-q"}, AllowExit: []int{5}}}}})
		}, "exit 5"},
		{"network for anything but npm ci", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "t", Argv: []string{"python", "-m", "pip", "install", "x"}, Network: true}}}})
		}, "network access"},
		{"npm install with network", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "t", Argv: []string{"npm", "install", "--ignore-scripts"}, Network: true}}}})
		}, "network access"},
		{"npm ci without ignore-scripts", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "t", Argv: []string{"npm", "ci"}, Network: true}}}})
		}, "network access"},
		{"allow_exit 1 on a required test gate", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "test", Argv: []string{"npm", "run", "test"}, AllowExit: []int{1}}}}})
		}, "allow_exit"},
		{"allow_exit 2 on typecheck", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "typecheck", Argv: []string{"npm", "run", "typecheck"}, AllowExit: []int{2}}}}})
		}, "allow_exit"},
		{"allow_exit on build", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "build", Argv: []string{"npm", "run", "build"}, AllowExit: []int{1, 2}}}}})
		}, "allow_exit"},
		{"setup requesting the network", func() BoardConfig {
			b := boardWith(nil)
			b.Setup = []Check{{Name: "fetch", Argv: []string{"npm", "ci", "--ignore-scripts"}, Network: true}}
			return b
		}, "setup"},
		{"setup running a shell", func() BoardConfig {
			b := boardWith(nil)
			b.Setup = []Check{{Name: "sh", Argv: []string{"bash", "-lc", "cat ~/.ssh/id_rsa"}}}
			return b
		}, "forbidden command"},
		{"required file escaping the worktree", func() BoardConfig {
			return boardWith([]Profile{{Name: "x", TitlePrefixes: []string{"[A"}, Checks: ok, RequireFiles: []string{"../../etc/passwd"}}})
		}, "escapes the worktree"},
		{"legacy board-wide pytest exit 5", func() BoardConfig {
			b := boardWith(nil)
			b.Checks = []Check{{Name: "unit", Argv: []string{"/v/bin/python", "-m", "pytest"}, AllowExit: []int{5}}}
			return b
		}, "exit 5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validate(t, tc.bc()); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("config error = %v, want %q", err, tc.want)
			}
		})
	}

	good := boardWith([]Profile{{Name: "core-frontend", TitlePrefixes: []string{"[V2 FE"}, RequireFiles: []string{"frontend/package-lock.json"},
		RequireNPM: &NPMRequire{Dir: "frontend", Scripts: []string{"test"}},
		Checks: []Check{
			{Name: "install", Argv: []string{"npm", "--prefix", "frontend", "ci", "--ignore-scripts"}, Network: true},
			{Name: "test", Argv: []string{"npm", "--prefix", "frontend", "run", "test"}},
		}}})
	if err := validate(t, good); err != nil {
		t.Fatalf("a valid frontend profile was refused: %v", err)
	}
	// Flags that belong to pytest, not to python, are fine after -m.
	py := boardWith([]Profile{{Name: "core-backend", TitlePrefixes: []string{"[V2 BE"}, Checks: []Check{
		{Name: "tests", Argv: []string{"/venv/bin/python", "-m", "pytest", "-q", "-p", "no:cacheprovider", "-c", "pytest.ini", "backend/tests/x.py"}},
		{Name: "compile", Argv: []string{"/venv/bin/python", "-m", "compileall", "-q", "backend/app"}},
	}}})
	if err := validate(t, py); err != nil {
		t.Fatalf("a valid backend profile was refused: %v", err)
	}
}

func TestProfileVerifyRefusesMissingLockfileAndScripts(t *testing.T) {
	p := Profile{Name: "core-frontend", RequireFiles: []string{"frontend/package-lock.json"},
		RequireNPM: &NPMRequire{Dir: "frontend", Scripts: []string{"test", "typecheck"}}}
	wt := t.TempDir()
	os.MkdirAll(filepath.Join(wt, "frontend"), 0o755)
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(wt, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Verify(wt); err == nil || !strings.Contains(err.Error(), "package-lock.json") {
		t.Fatalf("missing lockfile accepted: %v", err)
	}
	write("frontend/package-lock.json", "{}")
	if err := p.Verify(wt); err == nil || !strings.Contains(err.Error(), "package.json") {
		t.Fatalf("missing package.json accepted: %v", err)
	}
	write("frontend/package.json", `{"scripts":{"build":"vite build"}}`)
	if err := p.Verify(wt); err == nil || !strings.Contains(err.Error(), `"test"`) {
		t.Fatalf("missing test script accepted: %v", err)
	}
	for _, trivial := range []string{`echo ok`, `true`, `exit 0`, `vitest run || true`, ``} {
		write("frontend/package.json", `{"scripts":{"test":"`+trivial+`","typecheck":"tsc -b"}}`)
		if err := p.Verify(wt); err == nil || !strings.Contains(err.Error(), "no real work") && !strings.Contains(err.Error(), "no \"test\" script") {
			t.Fatalf("trivial script %q accepted: %v", trivial, err)
		}
	}
	write("frontend/package.json", `{"scripts":{"test":"vitest run","typecheck":"tsc -b"}}`)
	if err := p.Verify(wt); err != nil {
		t.Fatalf("real scripts and lockfile refused: %v", err)
	}
	// A symlinked lockfile is not a regular file in the worktree.
	os.Remove(filepath.Join(wt, "frontend", "package-lock.json"))
	os.Symlink("/etc/hostname", filepath.Join(wt, "frontend", "package-lock.json"))
	if err := p.Verify(wt); err == nil {
		t.Fatal("symlinked lockfile accepted")
	}
}

func fakeTool(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckerNeverCountsAnEmptySuiteAsPassing(t *testing.T) {
	pytest := fakeTool(t, "pytest", "echo 'no tests ran in 0.01s'; exit 5")
	vitest := fakeTool(t, "vitest", "echo 'No test files found, exiting with code 1'; exit 1")
	nodetest := fakeTool(t, "nodetest", "printf '# tests 0\\n# pass 0\\n'; exit 0")
	collected := fakeTool(t, "runner", "echo 'collected 0 items'; exit 0")
	allSkipped := fakeTool(t, "skipper", "echo '2 skipped in 0.03s'; exit 0")
	vitestSkipped := fakeTool(t, "vitestskip", "printf ' Test Files  1 skipped (1)\\n      Tests  3 skipped (3)\\n'; exit 0")
	missing := filepath.Join(t.TempDir(), "no-such-tool")
	checks := []Check{
		{Name: "pytest exit 5 even when allowed", Argv: []string{pytest}, AllowExit: []int{5}},
		{Name: "vitest finds nothing", Argv: []string{vitest}},
		{Name: "node:test with zero tests exits 0", Argv: []string{nodetest}},
		{Name: "collected 0 items exits 0", Argv: []string{collected}},
		{Name: "every test skipped (e.g. no Docker) exits 0", Argv: []string{allSkipped}},
		{Name: "vitest with every test skipped exits 0", Argv: []string{vitestSkipped}},
		{Name: "nonexistent executable", Argv: []string{missing}},
	}
	res, err := CmdChecker{Dir: t.TempDir(), Checks: checks, Unsandboxed: true}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(checks) {
		t.Fatalf("results = %d, want %d", len(res), len(checks))
	}
	for _, r := range res {
		if r.Passed {
			t.Errorf("%s: counted as PASS (exit %d, note %q)", r.Name, r.ExitCode, r.Note)
		}
	}
	if res[0].ExitCode != 5 || !strings.Contains(res[0].Note, "exit 5") {
		t.Errorf("pytest exit 5 result = %+v", res[0])
	}
	if res[6].ExitCode != -1 {
		t.Errorf("a missing executable should fail with exit -1, got %d", res[6].ExitCode)
	}
	good := fakeTool(t, "runner", "echo '3 passed, 1 skipped in 0.50s'; exit 0")
	res, _ = CmdChecker{Dir: t.TempDir(), Checks: []Check{{Name: "real", Argv: []string{good}}}, Unsandboxed: true}.Run(context.Background())
	if !res[0].Passed {
		t.Fatalf("a real passing run was rejected: %+v", res[0])
	}
}

func TestCheckerPreconditionFailureIsAFailedCheck(t *testing.T) {
	p := Profile{Name: "core-frontend", RequireFiles: []string{"frontend/package-lock.json"}}
	res, err := CmdChecker{Dir: t.TempDir(), Checks: []Check{{Name: "ok", Argv: []string{"true"}}}, Unsandboxed: true, Require: p.Verify}.Run(context.Background())
	if err != nil || len(res) != 1 || res[0].Passed || res[0].Name != "profile preconditions" || !strings.Contains(res[0].Output, "package-lock.json") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if gates := pipeline.CodeGates([]string{"a.txt"}, []string{"**"}, res); len(gates) == 0 {
		t.Fatal("a failed precondition produced no blocking gate")
	}
}

func TestSetupAndChecksDoNotInheritTheMembersSecrets(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "super-secret-value")
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	probe := fakeTool(t, "probe", `echo "key=${OPENCODE_API_KEY:-unset} gh=${GITHUB_TOKEN:-unset}"`)
	res, err := CmdChecker{Dir: t.TempDir(), Checks: []Check{{Name: "env", Argv: []string{probe}}}, Unsandboxed: true}.Run(context.Background())
	if err != nil || len(res) != 1 {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if strings.Contains(res[0].Output, "secret") || !strings.Contains(res[0].Output, "key=unset gh=unset") {
		t.Fatalf("secrets reached the command: %q", res[0].Output)
	}
}

func TestUnsandboxedChecksGetTheWorktreeAsHomeNotTheRealOne(t *testing.T) {
	real := t.TempDir()
	t.Setenv("HOME", real)
	wt := t.TempDir()
	probe := fakeTool(t, "probe", `echo "HOME=$HOME"`)
	res, _ := CmdChecker{Dir: wt, Checks: []Check{{Name: "home", Argv: []string{probe}}}, Unsandboxed: true}.Run(context.Background())
	if strings.Contains(res[0].Output, real) || !strings.Contains(res[0].Output, "HOME="+wt) {
		t.Fatalf("HOME leaked or wrong: %q", res[0].Output)
	}
}

func TestSandboxedSetupStyleCommandsStillWorkOffline(t *testing.T) {
	needBwrap(t)
	home, wt, _ := homeDirs(t)
	target := filepath.Join(home, "repo", "vendor")
	os.MkdirAll(target, 0o755)
	// The Lab board's setup is `ln -s <repo>/vendor vendor`: it must work inside
	// the sandbox even though the target lives under the hidden home.
	res, err := CmdChecker{Dir: wt, Sandbox: &Sandbox{Worktree: wt}, Checks: []Check{{Name: "link vendor", Argv: []string{"ln", "-s", target, "vendor"}}}}.Run(context.Background())
	if err != nil || !res[0].Passed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got, err := os.Readlink(filepath.Join(wt, "vendor")); err != nil || got != target {
		t.Fatalf("symlink on disk = %q %v", got, err)
	}
}

func TestOnlyTheExactLockedInstallMayUseTheNetwork(t *testing.T) {
	good := [][]string{
		{"npm", "ci", "--ignore-scripts"},
		{"npm", "--prefix", "frontend", "ci", "--ignore-scripts"},
	}
	for _, argv := range good {
		if _, ok := LockedNPMInstall(argv); !ok {
			t.Errorf("%v refused", argv)
		}
		if err := ValidateCheck(Check{Name: "install", Argv: argv, Network: true}); err != nil {
			t.Errorf("%v refused by config validation: %v", argv, err)
		}
	}
	bad := [][]string{
		{"npm", "ci", "--ignore-scripts", "--registry", "https://evil.example/"},
		{"npm", "ci", "--ignore-scripts", "--registry=https://evil.example/"},
		{"npm", "ci", "--ignore-scripts=false"},
		{"npm", "ci", "--ignore-scripts", "--ignore-scripts=false"},
		{"npm", "ci", "--ignore-scripts", "--userconfig", "/tmp/rc"},
		{"npm", "ci", "--ignore-scripts", "--globalconfig=/tmp/rc"},
		{"npm", "ci", "--ignore-scripts", "--global"},
		{"npm", "ci", "--ignore-scripts", "--foreground-scripts"},
		{"npm", "ci", "--ignore-scripts", "--cache", "/etc"},
		{"npm", "ci", "--ignore-scripts", "--prefix", "/etc"},
		{"npm", "--prefix", "/etc", "ci", "--ignore-scripts"},
		{"npm", "--prefix", "../../etc", "ci", "--ignore-scripts"},
		{"npm", "--prefix=frontend", "ci", "--ignore-scripts"},
		{"npm", "--prefix", "--registry", "ci", "--ignore-scripts"},
		{"npm", "ci"},
		{"npm", "--ignore-scripts", "ci"},
		{"npm", "install", "--ignore-scripts"},
		{"npm", "i", "--ignore-scripts"},
		{"/usr/bin/npm", "ci", "--ignore-scripts"},
		{"./npm", "ci", "--ignore-scripts"},
		{"yarn", "install", "--ignore-scripts"},
	}
	for _, argv := range bad {
		if _, ok := LockedNPMInstall(argv); ok {
			t.Errorf("hostile argv accepted by LockedNPMInstall: %v", argv)
		}
		if err := ValidateCheck(Check{Name: "install", Argv: argv, Network: true}); err == nil {
			t.Errorf("hostile argv accepted by ValidateCheck with network: %v", argv)
		}
	}
}

func lockfile(entries string) string {
	return `{"name":"f","lockfileVersion":3,"packages":{"":{"name":"f"}` + entries + `}}`
}

const goodEntry = `,"node_modules/zod":{"version":"4.6.7","resolved":"https://registry.npmjs.org/zod/-/zod-4.6.7.tgz","integrity":"sha512-AAAA"}`

func installTree(t *testing.T, lock string) string {
	t.Helper()
	wt := t.TempDir()
	os.MkdirAll(filepath.Join(wt, "frontend"), 0o755)
	if err := os.WriteFile(filepath.Join(wt, "frontend", "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	return wt
}

var installArgv = []string{"npm", "--prefix", "frontend", "ci", "--ignore-scripts"}

func TestNetworkInstallIsRefusedForLockfilesThatLeaveTheRegistry(t *testing.T) {
	if err := PreflightNetworkInstall(installTree(t, lockfile(goodEntry)), installArgv); err != nil {
		t.Fatalf("a clean lockfile was refused: %v", err)
	}
	v2 := strings.Replace(lockfile(goodEntry), `"lockfileVersion":3`, `"lockfileVersion":2`, 1)
	if err := PreflightNetworkInstall(installTree(t, v2), installArgv); err != nil {
		t.Fatalf("a clean lockfileVersion 2 was refused: %v", err)
	}
	// Real lockfiles carry bundled dependencies without a URL of their own.
	bundled := goodEntry +
		`,"node_modules/p":{"resolved":"https://registry.npmjs.org/p/-/p-1.tgz","integrity":"sha512-A"}` +
		`,"node_modules/p/node_modules/q":{"version":"1.0.0","inBundle":true}` +
		`,"node_modules/p/node_modules/q/node_modules/r":{"version":"1.0.0","inBundle":true}`
	if err := PreflightNetworkInstall(installTree(t, lockfile(bundled)), installArgv); err != nil {
		t.Fatalf("a bundled dependency under a registry package was refused: %v", err)
	}
	cases := map[string]string{
		"inBundle with no parent":                   lockfile(`,"node_modules/q":{"version":"1.0.0","inBundle":true}`),
		"inBundle but parent missing":               lockfile(`,"node_modules/p/node_modules/q":{"version":"1.0.0","inBundle":true}`),
		"inBundle under an off-host parent":         lockfile(`,"node_modules/p":{"resolved":"https://evil.example/p.tgz","integrity":"sha512-A"},"node_modules/p/node_modules/q":{"version":"1.0.0","inBundle":true}`),
		"inBundle under a parent with no integrity": lockfile(`,"node_modules/p":{"resolved":"https://registry.npmjs.org/p/-/p-1.tgz"},"node_modules/p/node_modules/q":{"version":"1.0.0","inBundle":true}`),
		"other https host":                          lockfile(`,"node_modules/x":{"resolved":"https://evil.example/x.tgz","integrity":"sha512-A"}`),
		"http registry":                             lockfile(`,"node_modules/x":{"resolved":"http://registry.npmjs.org/x/-/x-1.tgz","integrity":"sha512-A"}`),
		"lookalike host":                            lockfile(`,"node_modules/x":{"resolved":"https://registry.npmjs.org.evil.example/x.tgz","integrity":"sha512-A"}`),
		"git over ssh":                              lockfile(`,"node_modules/x":{"resolved":"git+ssh://git@github.com/o/r.git#abc","integrity":"sha512-A"}`),
		"github tarball":                            lockfile(`,"node_modules/x":{"resolved":"https://codeload.github.com/o/r/tar.gz/abc","integrity":"sha512-A"}`),
		"no resolved":                               lockfile(`,"node_modules/x":{"version":"1.0.0","integrity":"sha512-A"}`),
		"no integrity":                              lockfile(`,"node_modules/x":{"resolved":"https://registry.npmjs.org/x/-/x-1.tgz"}`),
		"weak integrity":                            lockfile(`,"node_modules/x":{"resolved":"https://registry.npmjs.org/x/-/x-1.tgz","integrity":"sha1-A"}`),
		"linked package":                            lockfile(`,"node_modules/x":{"resolved":"../x","link":true}`),
		"lockfile v1":                               `{"lockfileVersion":1,"dependencies":{}}`,
		"empty packages":                            `{"lockfileVersion":3,"packages":{}}`,
		"lockfile v4 (future, not evaluated)":       `{"lockfileVersion":4,"packages":{"":{},"node_modules/zod":{"resolved":"https://registry.npmjs.org/zod/-/zod-4.6.7.tgz","integrity":"sha512-AAAA"}}}`,
		"lockfile v0":                               `{"lockfileVersion":0,"packages":{"":{}}}`,
		"not json":                                  `lockfile`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if err := PreflightNetworkInstall(installTree(t, body), installArgv); err == nil {
				t.Fatalf("accepted: %s", body)
			}
		})
	}
}

func TestNetworkInstallIsRefusedWhenNPMConfigIsInTheWorktree(t *testing.T) {
	for _, rel := range []string{".npmrc", "frontend/.npmrc", "frontend/.yarnrc", ".pnpmrc"} {
		t.Run(rel, func(t *testing.T) {
			wt := installTree(t, lockfile(goodEntry))
			os.WriteFile(filepath.Join(wt, rel), []byte("@scope:registry=https://evil.example/\nignore-scripts=false\n"), 0o644)
			if err := PreflightNetworkInstall(wt, installArgv); err == nil || !strings.Contains(err.Error(), "registry") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if err := PreflightNetworkInstall(installTree(t, lockfile(goodEntry)), []string{"npm", "ci", "--ignore-scripts", "--registry=x"}); err == nil {
		t.Fatal("preflight accepted a non-allowlisted argv")
	}
}

func TestNetworkInstallNeverRunsWhenPreflightFails(t *testing.T) {
	wt := installTree(t, lockfile(`,"node_modules/x":{"resolved":"https://evil.example/x.tgz","integrity":"sha512-A"}`))
	marker := filepath.Join(wt, "ran")
	fakeBin := t.TempDir()
	os.WriteFile(filepath.Join(fakeBin, "npm"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	t.Setenv("PATH", fakeBin+":"+os.Getenv("PATH"))
	res, err := CmdChecker{Dir: wt, Sandbox: &Sandbox{Worktree: wt}, Checks: []Check{{Name: "install", Argv: installArgv, Network: true}}}.Run(context.Background())
	if err != nil || len(res) != 1 || res[0].Passed || !strings.Contains(res[0].Output, "refused before running") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the install command ran despite a failed preflight")
	}
}

func TestNetworkInstallRefusesSymlinkedProjectOrLockfile(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "package-lock.json"), []byte(lockfile(goodEntry)), 0o644)
	// frontend -> directory outside the worktree
	wt := t.TempDir()
	os.Symlink(outside, filepath.Join(wt, "frontend"))
	if err := PreflightNetworkInstall(wt, installArgv); err == nil || !strings.Contains(err.Error(), "outside the worktree") {
		t.Fatalf("symlinked project directory accepted: %v", err)
	}
	// lockfile -> file outside the worktree
	wt2 := t.TempDir()
	os.MkdirAll(filepath.Join(wt2, "frontend"), 0o755)
	os.Symlink(filepath.Join(outside, "package-lock.json"), filepath.Join(wt2, "frontend", "package-lock.json"))
	if err := PreflightNetworkInstall(wt2, installArgv); err == nil || !strings.Contains(err.Error(), "outside the worktree") {
		t.Fatalf("symlinked lockfile accepted: %v", err)
	}
}

func TestProfileVerifyFollowsSymlinksBeforeTrustingAPath(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "package.json"), []byte(`{"scripts":{"test":"vitest run"}}`), 0o644)
	os.WriteFile(filepath.Join(outside, "package-lock.json"), []byte("{}"), 0o644)
	p := Profile{Name: "x", RequireFiles: []string{"frontend/package-lock.json"}, RequireNPM: &NPMRequire{Dir: "frontend", Scripts: []string{"test"}}}

	// The directory itself is a symlink out of the worktree.
	wt := t.TempDir()
	os.Symlink(outside, filepath.Join(wt, "frontend"))
	if err := p.Verify(wt); err == nil || !strings.Contains(err.Error(), "outside the worktree") {
		t.Fatalf("symlinked directory accepted: %v", err)
	}
	// Real directory, but package.json points outside.
	wt2 := t.TempDir()
	os.MkdirAll(filepath.Join(wt2, "frontend"), 0o755)
	os.WriteFile(filepath.Join(wt2, "frontend", "package-lock.json"), []byte("{}"), 0o644)
	os.Symlink(filepath.Join(outside, "package.json"), filepath.Join(wt2, "frontend", "package.json"))
	if err := p.Verify(wt2); err == nil || !strings.Contains(err.Error(), "outside the worktree") {
		t.Fatalf("symlinked package.json accepted: %v", err)
	}
	// RequireNPM.Dir alone through a symlinked parent.
	p2 := Profile{Name: "x", RequireNPM: &NPMRequire{Dir: "ui/app", Scripts: []string{"test"}}}
	wt3 := t.TempDir()
	os.MkdirAll(filepath.Join(outside, "app"), 0o755)
	os.WriteFile(filepath.Join(outside, "app", "package.json"), []byte(`{"scripts":{"test":"vitest run"}}`), 0o644)
	os.Symlink(outside, filepath.Join(wt3, "ui"))
	if err := p2.Verify(wt3); err == nil || !strings.Contains(err.Error(), "outside the worktree") {
		t.Fatalf("symlinked parent of RequireNPM.Dir accepted: %v", err)
	}
	// Control: the same layout inside the worktree is accepted.
	wt4 := t.TempDir()
	os.MkdirAll(filepath.Join(wt4, "frontend"), 0o755)
	os.WriteFile(filepath.Join(wt4, "frontend", "package.json"), []byte(`{"scripts":{"test":"vitest run"}}`), 0o644)
	os.WriteFile(filepath.Join(wt4, "frontend", "package-lock.json"), []byte("{}"), 0o644)
	if err := p.Verify(wt4); err != nil {
		t.Fatalf("a normal worktree was refused: %v", err)
	}
}

func TestSandboxNetworkIsOffUnlessTheInstallCheckAsksForIt(t *testing.T) {
	wt := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	argv := []string{"npm", "ci", "--ignore-scripts"}
	off, err := Sandbox{Worktree: wt, Bwrap: "/usr/bin/bwrap"}.Wrap(argv)
	if err != nil {
		t.Skipf("sandbox wrap: %v", err)
	}
	if !contains(off, "--unshare-net") {
		t.Fatal("default sandbox shares the network")
	}
	for _, a := range off {
		if strings.HasPrefix(a, "npm_config_registry") {
			t.Fatal("registry pinned in a sandbox that has no network")
		}
	}
	on, _ := Sandbox{Worktree: wt, Bwrap: "/usr/bin/bwrap", Network: true}.Wrap(argv)
	if contains(on, "--unshare-net") {
		t.Fatal("network sandbox still unshares the network")
	}
	if !contains(on, "--clearenv") || !contains(on, AllowedNPMRegistry) || !contains(on, "npm_config_ignore_scripts") ||
		!contains(on, "npm_config_userconfig") || !contains(on, "npm_config_globalconfig") {
		t.Fatalf("network sandbox lost env clearing or registry pin: %v", on)
	}
	if !contains(on, "--tmpfs") {
		t.Fatal("home is not hidden")
	}
	if !contains(on, "/tmp/npm-userconfig") || !contains(on, "/tmp/npm-globalconfig") || contains(on, "/dev/null") {
		t.Fatalf("npm user/global config must be two distinct missing files, not /dev/null: %v", on)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestSandboxNetworkNamespaceRealRun(t *testing.T) {
	needBwrap(t)
	home, wt, _ := homeDirs(t)
	_ = home
	// The sandbox flag itself, probed directly: CmdChecker only turns it on for
	// the allowlisted install, after PreflightNetworkInstall.
	run := func(network bool) string {
		argv, err := Sandbox{Worktree: wt, Network: network}.Wrap([]string{"cat", "/proc/net/dev"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("probe failed: %v %s", err, out)
		}
		return string(out)
	}
	hostDev, _ := os.ReadFile("/proc/net/dev")
	hostHasMore := strings.Count(string(hostDev), ":") > 1
	isolated := run(false)
	if strings.Count(isolated, ":") > 1 {
		t.Fatalf("default sandbox sees host interfaces:\n%s", isolated)
	}
	if hostHasMore && strings.Count(run(true), ":") <= 1 {
		t.Fatal("network sandbox did not share the host network")
	}
}

func TestOutOfScopeAndSensitiveChangesAreBlocked(t *testing.T) {
	pass := []pipeline.CheckResult{{Name: "ok", Passed: true}}
	if g := pipeline.CodeGates([]string{"frontend/package.json", "frontend/src/main.tsx"}, []string{"frontend/package.json"}, pass); len(g) != 1 || g[0].File != "frontend/src/main.tsx" {
		t.Fatalf("out-of-scope file not blocked: %+v", g)
	}
	if g := pipeline.CodeGates([]string{".github/workflows/ci.yml"}, []string{"**"}, pass); len(g) == 0 {
		t.Fatal("CI file change not blocked")
	}
	if g := pipeline.CodeGates(nil, []string{"**"}, pass); len(g) == 0 {
		t.Fatal("an empty diff was not blocked")
	}
}

func TestSandboxedProfileCheckCannotReadTheMembersHome(t *testing.T) {
	needBwrap(t)
	home, wt, _ := homeDirs(t)
	probe := filepath.Join(wt, "probe.sh")
	os.WriteFile(probe, []byte("#!/bin/sh\ncat "+filepath.Join(home, ".ssh", "id_ed25519")+"\n"), 0o755)
	res, err := CmdChecker{Dir: wt, Sandbox: &Sandbox{Worktree: wt}, Checks: []Check{{Name: "reads-ssh", Argv: []string{probe}}}}.Run(context.Background())
	if err != nil || len(res) != 1 {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if res[0].Passed || strings.Contains(res[0].Output, "SECRET-KEY") {
		t.Fatalf("check read a forbidden location: %+v", res[0])
	}
}

func TestOutputAssertionsAndOwnershipAreValidatedAtConfigLoad(t *testing.T) {
	ok := Check{Name: "ok", Argv: []string{"true"}}
	cases := []struct {
		name string
		p    Profile
		want string
	}{
		{"bad require_output regex", Profile{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "t", Argv: []string{"true"}, RequireOutput: "(unclosed"}}}, "regular expression"},
		{"bad reject_output regex", Profile{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{{Name: "t", Argv: []string{"true"}, RejectOutput: "[z-a]"}}}, "regular expression"},
		{"allowed_paths absolute", Profile{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{ok}, AllowedPaths: []string{"/etc/passwd"}}, "allowed_paths"},
		{"allowed_paths parent escape", Profile{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{ok}, AllowedPaths: []string{"../outside"}}, "allowed_paths"},
		{"allowed_paths empty entry", Profile{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{ok}, AllowedPaths: []string{" "}}, "allowed_paths"},
		{"allowed_paths whole repository", Profile{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{ok}, AllowedPaths: []string{"**"}}, "whole repository"},
		{"allowed_paths dot", Profile{Name: "x", TitlePrefixes: []string{"[A"}, Checks: []Check{ok}, AllowedPaths: []string{"./"}}, "whole repository"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validate(t, boardWith([]Profile{tc.p})); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("config error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestProfileOwnsOnlyTheFilesItListsFromConfigNotFromTheAgent(t *testing.T) {
	p := Profile{Name: "core-frontend-toolchain", AllowedPaths: []string{
		"frontend/package.json", "frontend/package-lock.json", "frontend/tests/setup.ts", "frontend/tests/*.test.ts"}}
	if err := p.Owns([]string{"frontend/package.json", "frontend/package-lock.json", "frontend/tests/setup.ts", "frontend/tests/toolchain-smoke.test.ts"}); err != nil {
		t.Fatalf("owned files refused: %v", err)
	}
	for _, bad := range []string{"frontend/src/main.tsx", "backend/app/x.py", ".github/workflows/ci.yml", "frontend/package.json.bak", "frontend/tests/nested/x.test.ts", "frontend/../backend/x.py"} {
		if err := p.Owns([]string{"frontend/package.json", bad}); err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%q was not refused: %v", bad, err)
		}
	}
	if err := (Profile{Name: "unrestricted"}).Owns([]string{"anything"}); err != nil {
		t.Fatalf("a profile without allowed_paths must not restrict: %v", err)
	}
	// The checker turns an ownership violation into a failed check before any
	// command runs.
	ran := filepath.Join(t.TempDir(), "ran")
	probe := fakeTool(t, "probe", "touch "+ran)
	res, err := CmdChecker{Dir: t.TempDir(), Unsandboxed: true, Checks: []Check{{Name: "never", Argv: []string{probe}}},
		Owns: func(context.Context) error { return p.Owns([]string{"frontend/src/main.tsx"}) }}.Run(context.Background())
	if err != nil || len(res) != 1 || res[0].Passed || res[0].Name != "profile source ownership" || !strings.Contains(res[0].Output, "frontend/src/main.tsx") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a check ran although the profile does not own the change")
	}
}

func TestRequireAndRejectOutputCatchWhatAnExitCodeCannot(t *testing.T) {
	tap := func(tests, pass, fail, skipped, todo int) string {
		return "printf '# tests " + itoa(tests) + "\\n# suites 0\\n# pass " + itoa(pass) + "\\n# fail " + itoa(fail) + "\\n# cancelled 0\\n# skipped " + itoa(skipped) + "\\n# todo " + itoa(todo) + "\\n'; exit 0"
	}
	require := `(?m)^# tests ([5-9]|[1-9][0-9]+)$`
	reject := `(?m)^# (skipped|todo|cancelled) [1-9]`
	mk := func(name, body string) Check {
		return Check{Name: name, Argv: []string{fakeTool(t, name, body)}, RequireOutput: require, RejectOutput: reject}
	}
	checks := []Check{
		mk("six-real-tests", tap(6, 6, 0, 0, 0)),
		mk("empty-file-counts-as-one-test", tap(1, 1, 0, 0, 0)),
		mk("four-tests-is-too-few", tap(4, 4, 0, 0, 0)),
		mk("one-skipped", tap(6, 5, 0, 1, 0)),
		mk("one-todo", tap(6, 5, 0, 0, 1)),
		{Name: "bad-regex-at-runtime", Argv: []string{fakeTool(t, "x", "echo hi")}, RequireOutput: "(unclosed"},
	}
	res, err := CmdChecker{Dir: t.TempDir(), Checks: checks, Unsandboxed: true}.Run(context.Background())
	if err != nil || len(res) != len(checks) {
		t.Fatalf("res=%d err=%v", len(res), err)
	}
	want := []bool{true, false, false, false, false, false}
	for i, r := range res {
		if r.Passed != want[i] {
			t.Errorf("%s: passed=%v, want %v (note %q)", r.Name, r.Passed, want[i], r.Note)
		}
		if !r.Passed && r.Note == "" {
			t.Errorf("%s: failed without saying why", r.Name)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// The regexes above are only worth anything if they match what node really
// prints. This runs the real node:test runner (Node 24) on four files.
func TestNodeTestRealRunnerEmptySkippedAndShortSuitesAreRejected(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	head := "import test from 'node:test';\nimport assert from 'node:assert';\n"
	var six strings.Builder
	six.WriteString(head)
	for i := 0; i < 6; i++ {
		six.WriteString("test('t" + itoa(i) + "', () => assert.equal(1 + 1, 2));\n")
	}
	write("six.test.mjs", six.String())
	write("empty.test.mjs", "import test from 'node:test';\n")
	write("skip.test.mjs", six.String()+"test('s', { skip: true }, () => {});\n")
	write("short.test.mjs", head+"test('a', () => assert.ok(true));\ntest('b', () => assert.ok(true));\n")
	write("fail.test.mjs", six.String()+"test('bad', () => assert.equal(1, 2));\n")
	mk := func(file string) Check {
		return Check{Name: file, Argv: []string{node, "--test", "--test-reporter=tap", filepath.Join(dir, file)},
			RequireOutput: `(?m)^# tests ([5-9]|[1-9][0-9]+)$`, RejectOutput: `(?m)^# (skipped|todo|cancelled) [1-9]`}
	}
	res, err := CmdChecker{Dir: dir, Unsandboxed: true, Checks: []Check{mk("six.test.mjs"), mk("empty.test.mjs"), mk("skip.test.mjs"), mk("short.test.mjs"), mk("fail.test.mjs")}}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"six.test.mjs": true, "empty.test.mjs": false, "skip.test.mjs": false, "short.test.mjs": false, "fail.test.mjs": false}
	for _, r := range res {
		if r.Passed != want[r.Name] {
			t.Errorf("%s: passed=%v (exit %d, note %q), want %v\n%s", r.Name, r.Passed, r.ExitCode, r.Note, want[r.Name], r.Output)
		}
	}
	// Without the assertions node:test passes an empty file and skipped tests,
	// which is exactly the gap the assertions close.
	bare, _ := CmdChecker{Dir: dir, Unsandboxed: true, Checks: []Check{
		{Name: "empty", Argv: []string{node, "--test", "--test-reporter=tap", filepath.Join(dir, "empty.test.mjs")}},
		{Name: "skip", Argv: []string{node, "--test", "--test-reporter=tap", filepath.Join(dir, "skip.test.mjs")}},
	}}.Run(context.Background())
	for _, r := range bare {
		if !r.Passed {
			t.Skipf("this node version already fails %s (%s); the assertions are redundant here", r.Name, r.Output)
		}
	}
}

// Vitest 5.1 prints its summary with colour codes even under CI=1, so the
// patterns must see plain text. These are the real summary lines with the
// escape sequences Vitest emits around them.
func TestAssertionsAndEmptySuiteMarkersSeeThroughColourCodes(t *testing.T) {
	const esc = "\x1b"
	line := func(s string) string {
		return "printf '" + esc + "[2m     Tests " + esc + "[22m " + esc + "[1m" + esc + "[32m" + s + esc + "[39m" + esc + "[22m\\n'; exit 0"
	}
	require := `(?m)^\s*Tests\s+[1-9][0-9]* passed`
	reject := `(?m)^\s*Tests\s.*\b(skipped|todo|failed)\b`
	mk := func(name, body string) Check {
		return Check{Name: name, Argv: []string{fakeTool(t, name, body)}, RequireOutput: require, RejectOutput: reject}
	}
	checks := []Check{
		mk("coloured-pass", line("3 passed (3)")),
		mk("coloured-skip", line("1 passed | 1 skipped | 1 todo (3)")),
		mk("coloured-no-tests", line("no tests")),
		{Name: "coloured-zero-marker", Argv: []string{fakeTool(t, "z", "printf '"+esc+"[33mNo test files found"+esc+"[39m, exiting\\n'; exit 0")}},
	}
	res, err := CmdChecker{Dir: t.TempDir(), Checks: checks, Unsandboxed: true}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, false, false}
	for i, r := range res {
		if r.Passed != want[i] {
			t.Errorf("%s: passed=%v, want %v (note %q)", r.Name, r.Passed, want[i], r.Note)
		}
		if strings.Contains(r.Output, esc) {
			t.Errorf("%s: reported output still carries an escape code: %q", r.Name, r.Output)
		}
	}
}

// The migration example in the docs is what the founder will actually apply, so
// it is parsed, validated and routed here instead of trusted.
func TestDocsMigrationExampleIsValid(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "autopilot-check-profiles.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	start := strings.Index(text, "```json\n")
	end := strings.Index(text[start+8:], "```")
	if start < 0 || end < 0 {
		t.Fatal("no json block in the docs")
	}
	var bc BoardConfig
	if err := json.Unmarshal([]byte(text[start+8:start+8+end]), &bc); err != nil {
		t.Fatalf("docs JSON does not parse into a BoardConfig: %v", err)
	}
	bc.RepoName, bc.RepoDir, bc.ReadyList, bc.Label, bc.SandboxReadOnly = "birunidev-space", "/tmp", "Autopilot Queue", "none", nil
	if err := validate(t, bc); err != nil {
		t.Fatalf("the documented migration is refused by config validation: %v", err)
	}
	route := map[string]string{
		"[V2 FE TOOLCHAIN] Pinned FE toolchain, Axios/TanStack Router/Vitest deps, npm lock and checks": "core-frontend-toolchain",
		"[V2 FE TAILWIND] Clean-room Tailwind v4 Core CSS and shadcn design tokens":                     "core-frontend-tailwind",
		"[V2 FE QA] BRD-01 isolated preview fixture verification — child of BII-410":                    "core-frontend",
		"[V2 BE QA] Boards cross-tenant PostgreSQL negative test — child of BII-491":                    "core-backend",
	}
	for title, want := range route {
		if p, err := bc.ProfileFor(title); err != nil || p.Name != want {
			t.Errorf("%q -> %q (%v), want %q", title, p.Name, err, want)
		}
	}
	if _, err := bc.ProfileFor("[V2 FE ROUTES] something new"); !errors.Is(err, ErrNoProfile) {
		t.Errorf("an undefined frontend slice must be refused, got %v", err)
	}
	// BII-898 and BII-899 own disjoint files and neither is the whole repo.
	own := map[string]map[string]bool{}
	for _, p := range bc.Profiles {
		if p.Name != "core-frontend-toolchain" && p.Name != "core-frontend-tailwind" {
			continue
		}
		own[p.Name] = map[string]bool{}
		for _, f := range p.AllowedPaths {
			own[p.Name][f] = true
		}
	}
	for f := range own["core-frontend-toolchain"] {
		if own["core-frontend-tailwind"][f] {
			t.Errorf("BII-898 and BII-899 both own %s", f)
		}
	}
	if len(own["core-frontend-toolchain"]) != 6 || len(own["core-frontend-tailwind"]) != 3 {
		t.Errorf("file leases = %d and %d, want 6 and 3", len(own["core-frontend-toolchain"]), len(own["core-frontend-tailwind"]))
	}
	tailwind, _ := bc.ProfileFor("[V2 FE TAILWIND] x")
	if err := tailwind.Owns([]string{"frontend/package.json"}); err == nil {
		t.Error("the Tailwind profile must not own package.json (BII-898's lease)")
	}
	toolchain, _ := bc.ProfileFor("[V2 FE TOOLCHAIN] x")
	if err := toolchain.Owns([]string{"frontend/src/styles/app.css"}); err == nil {
		t.Error("the toolchain profile must not own the CSS files (BII-899's lease)")
	}
}
