package projects

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/engine"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard"
)

func mapping() engine.BoardConfig {
	return engine.BoardConfig{Name: "sample", RepoName: "sample", RepoDir: "/project", BoardID: "b1", Base: "feature/base", ReadyList: "Autopilot Queue", ReviewList: "Review", BlockedList: "On Hold", ClarifyList: "Needs Clarification", DoneList: "Done", Label: "none", Checks: []engine.Check{{Name: "tests", Argv: []string{"go", "test", "./..."}}}}
}

type fakeSpace struct {
	*board.Fake
	info  spaceboard.BoardInfo
	calls int
}

func (f *fakeSpace) GetBoard(context.Context, string) (spaceboard.BoardInfo, error) {
	f.calls++
	return f.info, nil
}
func boardInfo() spaceboard.BoardInfo {
	return spaceboard.BoardInfo{ID: "b1", Name: "Sample board", Description: "Implementation needs founder approval; follow card scope.", Lists: []spaceboard.List{{ID: "queue", Name: "Autopilot Queue"}, {ID: "review", Name: "Review"}, {ID: "hold", Name: "On Hold"}, {ID: "done", Name: "Done"}}}
}

func TestBoardMetadataFailClosedAndReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		edit   func(*spaceboard.BoardInfo)
		policy []string
		want   string
	}{
		{"valid with fallback", func(*spaceboard.BoardInfo) {}, nil, ""},
		{"wrong board", func(b *spaceboard.BoardInfo) { b.ID = "another" }, nil, "does not match"},
		{"missing queue", func(b *spaceboard.BoardInfo) { b.Lists = b.Lists[1:] }, nil, "ready list"},
		{"missing Done", func(b *spaceboard.BoardInfo) { b.Lists = b.Lists[:3] }, nil, "done list"},
		{"duplicate names", func(b *spaceboard.BoardInfo) {
			b.Lists = append(b.Lists, spaceboard.List{ID: "q2", Name: "AUTOPILOT QUEUE"})
		}, nil, "multiple lists"},
		{"Done ID alias", func(b *spaceboard.BoardInfo) { b.Lists[0].ID = "done" }, nil, "into Done"},
		{"no policy", func(b *spaceboard.BoardInfo) { b.Description = "" }, nil, "policy description"},
		{"missing policy card", func(*spaceboard.BoardInfo) {}, []string{"MISSING"}, "policy card"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := boardInfo()
			tc.edit(&info)
			api := &fakeSpace{Fake: board.NewFake(), info: info}
			b := mapping()
			b.PolicyCards = tc.policy
			r, err := ValidateBoard(context.Background(), api, b)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if !r.ClarifyFallback || r.Clarify != "hold" || !strings.Contains(r.Policy, "founder approval") {
					t.Fatal("policy/fallback lost")
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
			if len(api.Moves) != 0 || len(api.Comments) != 0 {
				t.Fatal("metadata wrote to board")
			}
			if api.calls != 1 {
				t.Fatal("board fetched repeatedly")
			}
		})
	}
}

func TestMappingGuards(t *testing.T) {
	if err := ValidateMapping(mapping()); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*engine.BoardConfig){func(b *engine.BoardConfig) { b.Name = "../escape" }, func(b *engine.BoardConfig) { b.Name = "all" }, func(b *engine.BoardConfig) { b.RepoName = "../escape" }, func(b *engine.BoardConfig) { b.Checks = nil }, func(b *engine.BoardConfig) { b.Checks[0].Argv = nil }, func(b *engine.BoardConfig) { b.Base = "" }, func(b *engine.BoardConfig) { b.ReviewList = "Done" }, func(b *engine.BoardConfig) { b.ReadyList = "Todo" }} {
		b := mapping()
		edit(&b)
		if ValidateMapping(b) == nil {
			t.Fatal("unsafe mapping accepted")
		}
	}
}

func TestSelectImportsExistingNamesAndRejectsAmbiguity(t *testing.T) {
	b := mapping()
	b.Name = "V2 Engineering"
	c := engine.Config{Boards: []engine.BoardConfig{b}}
	for _, ref := range []string{"v2 engineering", "sample", "b1"} {
		got, err := Select(c, ref)
		if err != nil || got.BoardID != "b1" {
			t.Fatalf("select %q: %v", ref, err)
		}
	}
	if _, err := Select(c, "missing"); err == nil {
		t.Fatal("unknown project selected")
	}
	other := mapping()
	other.BoardID = "b2"
	other.Name = "different"
	c.Boards = append(c.Boards, other)
	if _, err := Select(c, "sample"); err == nil {
		t.Fatal("ambiguous repository selected")
	}
}

func TestSavePreservesSettingsStopAndLedger(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	lockPath := filepath.Join(root, "autopilot.lock")
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	doc.Config.Model = "preserved/model"
	doc.Config.DailyTokens = 54321
	for _, name := range []string{"STOP", "usage.json", "status.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	next, err := Append(doc.Config, mapping())
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, lockPath, doc, next); err != nil {
		t.Fatal(err)
	}
	saved, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Config.Model != "preserved/model" || saved.Config.DailyTokens != 54321 || len(saved.Config.Boards) != 1 {
		t.Fatal("settings lost")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("configuration not private")
	}
	for _, name := range []string{"STOP", "usage.json", "status.json"} {
		b, _ := os.ReadFile(filepath.Join(root, name))
		if string(b) != "preserve" {
			t.Fatal("worker state changed")
		}
	}
	for _, edit := range []func(*engine.BoardConfig){func(b *engine.BoardConfig) { b.BoardID = "b1" }, func(b *engine.BoardConfig) { b.Name = "sample" }, func(b *engine.BoardConfig) { b.RepoName = "sample" }, func(b *engine.BoardConfig) { b.RepoDir = "/project" }} {
		b := mapping()
		b.Name = "other"
		b.RepoName = "other"
		b.RepoDir = "/other"
		b.BoardID = "b2"
		edit(&b)
		if _, err := Append(saved.Config, b); err == nil {
			t.Fatal("duplicate accepted")
		}
	}
	lock, err := runstate.AcquireLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, lockPath, saved, saved.Config); !errors.Is(err, runstate.ErrLocked) {
		t.Fatalf("live lock bypassed: %v", err)
	}
	lock.Release()
	newer := append(append([]byte(nil), saved.Raw...), '\n')
	os.WriteFile(path, newer, 0600)
	if err := Save(path, lockPath, saved, saved.Config); err == nil {
		t.Fatal("concurrent edit overwritten")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(newer) {
		t.Fatal("newer config lost")
	}
}

func TestLoadRejectsUnknownAndTrailingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, raw := range []string{`{"unexpected":true}`, `{} {}`, `null`, `{"boards":[]}`} {
		os.WriteFile(path, []byte(raw), 0600)
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted malformed config: %s", raw)
		}
	}
}

func TestRepositoryValidationDoesNotChangeCheckout(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	repo := filepath.Join(root, "repo with spaces")
	os.Mkdir(repo, 0700)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	head := git("rev-parse", "HEAD")
	if _, err := ValidateRepo(context.Background(), repo, "main"); err == nil {
		t.Fatal("missing remote accepted")
	}
	git("remote", "add", "origin", "https://github.com/example/fixture.git")
	if _, err := ValidateRepo(context.Background(), repo, "main"); err == nil {
		t.Fatal("missing remote base accepted")
	}
	git("update-ref", "refs/remotes/origin/main", head)
	got, err := ValidateRepo(context.Background(), repo, "main")
	if err != nil || got != repo {
		t.Fatalf("repo: %v", err)
	}
	if _, err := ValidateRepo(context.Background(), repo, "--bad"); err == nil {
		t.Fatal("invalid base accepted")
	}
	sub := filepath.Join(repo, "sub")
	os.Mkdir(sub, 0700)
	if _, err := ValidateRepo(context.Background(), sub, "main"); err == nil {
		t.Fatal("subdirectory accepted as repository root")
	}
	if git("rev-parse", "HEAD") != head || git("status", "--porcelain") != "?? sub/" && git("status", "--porcelain") != "" {
		t.Fatal("validation changed checkout")
	}
}

func TestReadOnlyDependenciesCannotExposeHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, path := range []string{"/", home} {
		if _, err := ValidateReadOnly([]string{path}); err == nil {
			t.Fatal("exposed root/home")
		}
	}
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(dir, link)
	paths, err := ValidateReadOnly([]string{link})
	if err != nil || paths[0] != dir {
		t.Fatalf("dependency: %v", err)
	}
}
