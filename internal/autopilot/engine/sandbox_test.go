package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	if out, err := exec.Command("bwrap", "--ro-bind", "/", "/", "true").CombinedOutput(); err != nil {
		t.Skipf("bwrap unusable here: %v %s", err, out)
	}
}

// homeDirs creates a fake home with a planted secret, and a worktree and a
// read-only dependency dir inside it, like the real layout.
func homeDirs(t *testing.T) (home, wt, dep string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("SECRET-KEY"), 0o600)
	wt = filepath.Join(home, ".theboringfloor", "autopilot", "worktrees", "space", "BII-1")
	dep = filepath.Join(home, ".theboringfloor", "autopilot", "venvs", "space")
	os.MkdirAll(wt, 0o700)
	os.MkdirAll(dep, 0o700)
	os.WriteFile(filepath.Join(dep, "lib.txt"), []byte("dep-ok"), 0o644)
	return
}

func runIn(t *testing.T, sb Sandbox, script string) (string, bool) {
	t.Helper()
	res, err := CmdChecker{Dir: sb.Worktree, Sandbox: &sb, Checks: []Check{{Name: "probe", Argv: []string{"sh", "-c", script}}}}.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res[0].Output, res[0].Passed
}

func TestSandboxHidesHomeAndNetwork(t *testing.T) {
	needBwrap(t)
	home, wt, dep := homeDirs(t)
	sb := Sandbox{Worktree: wt, ReadOnly: []string{dep}}

	if out, _ := runIn(t, sb, "cat "+filepath.Join(home, ".ssh", "id_ed25519")+" 2>&1; ls -A "+home); strings.Contains(out, "SECRET-KEY") {
		t.Fatalf("secret visible inside the sandbox:\n%s", out)
	}
	if out, ok := runIn(t, sb, "echo hi > out.txt && cat out.txt"); !ok || !strings.Contains(out, "hi") {
		t.Fatalf("worktree not writable: %s", out)
	}
	if _, err := os.Stat(filepath.Join(wt, "out.txt")); err != nil {
		t.Fatal("worktree write did not land on disk")
	}
	if out, ok := runIn(t, sb, "cat "+filepath.Join(dep, "lib.txt")); !ok || !strings.Contains(out, "dep-ok") {
		t.Fatalf("read-only dependency not readable: %s", out)
	}
	if _, ok := runIn(t, sb, "echo x > "+filepath.Join(dep, "evil.txt")); ok {
		t.Fatal("read-only dependency was writable")
	}
	if _, ok := runIn(t, sb, "echo x > /etc/autopilot-sandbox-test"); ok {
		t.Fatal("system path was writable")
	}
	// Outside the worktree the write may "succeed" into the sandbox's
	// private tmpfs, but it must never reach the real disk.
	outside := filepath.Join(filepath.Dir(home), "autopilot-escape-test")
	runIn(t, sb, "echo x > "+outside)
	if _, err := os.Stat(outside); err == nil {
		os.Remove(outside)
		t.Fatal("a write outside the worktree reached the real disk")
	}
	secretOut := filepath.Join(home, ".ssh", "authorized_keys")
	runIn(t, sb, "mkdir -p "+filepath.Dir(secretOut)+" && echo pwned > "+secretOut)
	if _, err := os.Stat(secretOut); err == nil {
		t.Fatal("a write into the real home directory reached the disk")
	}
	// /proc/net/dev reflects the process's own network namespace: only lo.
	out, _ := runIn(t, sb, "tail -n +3 /proc/net/dev | cut -d: -f1 | tr -d ' '")
	for _, iface := range strings.Fields(out) {
		if iface != "lo" {
			t.Fatalf("network interface %q visible inside the sandbox (all: %q)", iface, out)
		}
	}
}

func TestSandboxRequired(t *testing.T) {
	_, err := CmdChecker{Dir: t.TempDir(), Checks: []Check{{Name: "x", Argv: []string{"true"}}}}.Run(context.Background())
	if !errors.Is(err, ErrNoSandbox) {
		t.Fatalf("checks without a sandbox must refuse to run, got %v", err)
	}
	if _, err := (Sandbox{Worktree: t.TempDir(), Bwrap: "", ReadOnly: []string{"/"}}).Wrap([]string{"true"}); err == nil {
		t.Fatal("exposing / must be refused")
	}
	home, _ := os.UserHomeDir()
	if _, err := (Sandbox{Worktree: t.TempDir(), ReadOnly: []string{home}}).Wrap([]string{"true"}); err == nil {
		t.Fatal("exposing the whole home must be refused")
	}
}

func TestSandboxFailingCheckIsReported(t *testing.T) {
	needBwrap(t)
	_, wt, _ := homeDirs(t)
	out, ok := runIn(t, Sandbox{Worktree: wt}, "echo boom; exit 3")
	if ok || !strings.Contains(out, "boom") {
		t.Fatalf("ok=%v out=%q", ok, out)
	}
}

func TestSandboxClearsEnvironment(t *testing.T) {
	needBwrap(t)
	_, wt, _ := homeDirs(t)
	t.Setenv("GITHUB_TOKEN", "ghp_SHOULD_NOT_LEAK_0123456789")
	out, ok := runIn(t, Sandbox{Worktree: wt}, "env")
	if !ok || strings.Contains(out, "SHOULD_NOT_LEAK") || !strings.Contains(out, "CI=1") {
		t.Fatalf("environment not cleared (ok=%v):\n%s", ok, out)
	}
}

// A required gate passes on exit 0 only. allow_exit is refused at config load
// (see TestProfileConfigRefusals) and ignored here, so even a stale config that
// still carries it cannot turn a failing exit into a pass.
func TestAllowExitCanNoLongerTurnAFailureIntoAPass(t *testing.T) {
	needBwrap(t)
	_, wt, _ := homeDirs(t)
	sb := Sandbox{Worktree: wt}
	res, err := CmdChecker{Dir: wt, Sandbox: &sb, Checks: []Check{
		{Name: "no tests", Argv: []string{"sh", "-c", "exit 5"}, AllowExit: []int{5}},
		{Name: "real fail", Argv: []string{"sh", "-c", "exit 1"}, AllowExit: []int{1, 5}},
		{Name: "typecheck fail", Argv: []string{"sh", "-c", "exit 2"}, AllowExit: []int{2}},
	}}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Passed {
			t.Fatalf("an allow_exit configuration turned exit %d into a pass: %+v", r.ExitCode, r)
		}
	}
}
