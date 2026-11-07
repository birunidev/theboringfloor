package panels

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
)

func autopilotFixture(t *testing.T) (runstate.Paths, time.Time) {
	t.Helper()
	p := runstate.Paths{Root: t.TempDir()}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for name, value := range map[string]any{
		"config.json": map[string]any{"daily_tokens": 100000, "private": "must never render"},
		"usage.json":  runstate.Usage{Day: now.Format("2006-01-02"), Today: 1234},
	} {
		b, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(p.Root, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return p, now
}

func TestAutopilotReadsTailAndOnlyTogglesStop(t *testing.T) {
	p, now := autopilotFixture(t)
	log, err := p.OpenRunLog("BII-812", "fixture", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if err := log.Write(runstate.Event{Kind: "stage", Msg: fmt.Sprintf("event-%02d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	log.Close()
	if err := p.WriteStatus(runstate.Status{State: "running", Card: "BII-812", Stage: "review", Round: 2, Tokens: 99, LogPath: log.Path()}, now); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	filepath.WalkDir(p.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() {
			b, _ := os.ReadFile(path)
			before[path] = string(b)
		}
		return nil
	})
	a := NewAutopilot(p.Root)
	a.Update(a.Refresh()())
	if len(a.data.events) != 20 || !strings.Contains(a.data.events[0], "event-05") || !strings.Contains(a.data.events[19], "event-24") {
		t.Fatalf("tail: %v", a.data.events)
	}
	if a.data.today != 1234 || a.data.cap != 100000 {
		t.Fatalf("budget: %+v", a.data)
	}
	a.SetSize(70, 12)
	if !strings.Contains(a.View(), "event-24") {
		t.Fatal("latest event not visible")
	}
	for i := 0; i < 19; i++ {
		a.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if !strings.Contains(a.View(), "event-05") {
		t.Fatal("cannot scroll to oldest retained event")
	}
	for i := 0; i < 19; i++ {
		a.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if !strings.Contains(a.View(), "event-24") {
		t.Fatal("cannot return to latest event")
	}
	for _, stopped := range []bool{true, false} {
		cmd := a.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		if cmd == nil {
			t.Fatal("switch ignored")
		}
		a.Update(cmd())
		if p.Stopped() != stopped || a.data.stopped != stopped {
			t.Fatalf("STOP = %v, want %v", p.Stopped(), stopped)
		}
	}
	for path, want := range before {
		got, _ := os.ReadFile(path)
		if string(got) != want {
			t.Fatalf("panel mutated %s", path)
		}
	}
	filepath.WalkDir(p.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() {
			if _, ok := before[path]; !ok {
				t.Fatalf("panel created %s", path)
			}
		}
		return nil
	})
}

func TestAutopilotMissingCorruptAndYesterday(t *testing.T) {
	p, now := autopilotFixture(t)
	u := runstate.Usage{Day: now.AddDate(0, 0, -1).Format("2006-01-02"), Today: 999}
	b, _ := json.Marshal(u)
	os.WriteFile(p.UsageFile(), b, 0600)
	if r := readAutopilot(p, now); r.today != 0 || r.status.State != "idle" || r.err != "" {
		t.Fatalf("yesterday: %+v", r)
	}
	os.WriteFile(p.StatusFile(), []byte("broken json"), 0600)
	if r := readAutopilot(p, now); !strings.Contains(r.err, "status:") {
		t.Fatal("corrupt status hidden")
	}
	empty := runstate.Paths{Root: filepath.Join(t.TempDir(), "absent")}
	r := readAutopilot(empty, now)
	if r.status.State != "idle" || r.capKnown || r.stopped {
		t.Fatalf("missing: %+v", r)
	}
	if _, err := os.Stat(empty.Root); !os.IsNotExist(err) {
		t.Fatal("read created root")
	}
}

func TestAutopilotLogBoundaryAndPartialRecord(t *testing.T) {
	p, _ := autopilotFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte("secret"), 0600)
	for _, path := range []string{outside, filepath.Join(p.RunsDir(), "link.jsonl")} {
		if path != outside {
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := autopilotEvents(p, path); err == nil {
			t.Fatal("outside log accepted")
		}
	}
	path := filepath.Join(p.RunsDir(), "tail.jsonl")
	os.WriteFile(path, []byte(strings.Repeat("x", 300000)+"\n{\"kind\":\"end\",\"msg\":\"safe\\u001b[31m\\nrow\"}\n{\"kind\":"), 0600)
	events, err := autopilotEvents(p, path)
	if err != nil || len(events) != 1 || strings.ContainsAny(events[0], "\x1b\n") {
		t.Fatalf("tail = %q, %v", events, err)
	}
}

func TestAutopilotRenderStatesAndSizes(t *testing.T) {
	a := NewAutopilot(t.TempDir())
	for _, size := range [][2]int{{1, 1}, {24, 8}, {44, 24}, {100, 35}} {
		a.SetSize(size[0], size[1])
		for _, d := range []AutopilotLoadedMsg{{status: runstate.Status{State: "idle"}}, {status: runstate.Status{State: "running", Card: "BII-812", Stage: "review", Round: 2}, stopped: true, err: "status unreadable; retrying"}} {
			a.data = d
			for _, loaded := range []bool{false, true} {
				a.loaded = loaded
				frame := a.View()
				if strings.Count(frame, "\n")+1 != size[1] {
					t.Fatalf("height %v: %q", size, frame)
				}
				for _, line := range strings.Split(frame, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatalf("overflow %v: %q", size, line)
					}
				}
			}
		}
	}
}
