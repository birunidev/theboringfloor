package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/chrome"
	"github.com/theboringhumane/theboringfloor/internal/panels"
)

func autopilotModel(t *testing.T) (Model, runstate.Paths) {
	t.Helper()
	p := runstate.Paths{Root: t.TempDir()}
	t.Setenv("THEFLOOR_AUTOPILOT_HOME", p.Root)
	m := cockpitModel(t)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	log, err := p.OpenRunLog("BII-812", "render-fixture", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Write(runstate.Event{Kind: "stage", Stage: "review", Msg: "Fixture: reviewing policy tests"}); err != nil {
		t.Fatal(err)
	}
	log.Close()
	if err := p.WriteStatus(runstate.Status{State: "running", Card: "BII-812", Stage: "review", Round: 1, Tokens: 1234, LogPath: log.Path()}, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Root, "config.json"), []byte(`{"daily_tokens":100000}`), 0600); err != nil {
		t.Fatal(err)
	}
	m = runMsg(t, m, m.autopilot.Refresh()())
	return m, p
}

// Like workspaceshot, this renders the actual office frame with isolated disk
// fixtures and no model server. Every theme and narrow/wide layout is exercised.
func TestAutopilotWorkspaceshot(t *testing.T) {
	m, _ := autopilotModel(t)
	if !m.SelectTab("autopilot") {
		t.Fatal("autopilot tab missing")
	}
	for _, theme := range chrome.ThemeNames() {
		chrome.SetTheme(theme)
		for _, size := range [][2]int{{40, 18}, {70, 24}, {140, 40}, {220, 60}} {
			m.resize(size[0], size[1])
			m.focusPanel = true
			m.frameNonce++
			frame := m.Frame()
			bootFrameShape(t, frame, size[0], size[1])
			plain := ansi.Strip(frame)
			for _, want := range []string{"AUTOPILOT", "STOP: clear", "BII-812", "Run tokens: 1234"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("%s %v lost %q\n%s", theme, size, want, plain)
				}
			}
			if size[0] >= 140 && !strings.Contains(plain, "Fixture: reviewing policy tests") {
				t.Fatalf("%s lost log", theme)
			}
			if again := m.Frame(); again != frame {
				t.Fatal("unchanged frame differs")
			}
		}
	}
	chrome.SetTheme("cockpit")
	m.resize(140, 40)
	m.frameNonce++
	t.Log("isolated autopilot cockpit frame:\n" + ansi.Strip(m.Frame()))
}

func TestAutopilotAppKeysAndBackgroundRefresh(t *testing.T) {
	m, p := autopilotModel(t)
	m = runMsg(t, m, slashMsg{text: "/autopilot"})
	if m.tabs.ActiveIndex() != 8 {
		t.Fatal("slash did not select autopilot")
	}
	m.SelectTab("agents")
	m = runMsg(t, m, tea.KeyPressMsg{Code: '9', Text: "9"})
	if m.tabs.ActiveIndex() != 8 {
		t.Fatal("digit 9 did not select autopilot")
	}
	for _, stopped := range []bool{true, false} {
		next, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		m = next.(Model)
		if cmd == nil {
			t.Fatal("app swallowed STOP key")
		}
		// Update wraps panel commands in a tea.Batch.
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					m = runMsg(t, m, c())
				}
			}
		} else {
			m = runMsg(t, m, msg)
		}
		if p.Stopped() != stopped {
			t.Fatalf("STOP = %v, want %v", p.Stopped(), stopped)
		}
	}
	m.SelectTab("chat")
	next, cmd := m.Update(panels.AutopilotTickMsg{})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("hidden panel stopped polling")
	}
	// Complete only the refresh command; the other batch member is the timer.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("poll not rearmed: %T", cmd())
	}
	m = runMsg(t, m, batch[1]())
	m.SelectTab("autopilot")
	m.resize(140, 40)
	if !strings.Contains(ansi.Strip(m.Frame()), "STOP: clear") {
		t.Fatal("hidden refresh failed")
	}
}
