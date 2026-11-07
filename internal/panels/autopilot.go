package panels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/chrome"
	"github.com/theboringhumane/theboringfloor/internal/state"
)

const autopilotPoll = 3 * time.Second

type AutopilotTickMsg struct{}
type AutopilotLoadedMsg struct {
	status     runstate.Status
	today, cap int64
	capKnown   bool
	stopped    bool
	events     []string
	err        string
}

// Autopilot reads the worker's local files, independently of the office backend.
// Its only mutation is the explicit STOP key; polling never opens a Budget.
type Autopilot struct {
	paths        runstate.Paths
	w, h         int
	loaded, busy bool
	data         AutopilotLoadedMsg
	offset       int
}

func NewAutopilot(root string) *Autopilot       { return &Autopilot{paths: runstate.Paths{Root: root}} }
func (a *Autopilot) Title() string              { return "autopilot" }
func (a *Autopilot) SetSize(w, h int)           { a.w, a.h = w, h }
func (a *Autopilot) SetState(state.OfficeState) {}

func AutopilotTick() tea.Cmd {
	return tea.Tick(autopilotPoll, func(time.Time) tea.Msg { return AutopilotTickMsg{} })
}

func (a *Autopilot) Refresh() tea.Cmd {
	if a.busy {
		return nil
	}
	a.busy = true
	paths := a.paths
	return func() tea.Msg { return readAutopilot(paths, time.Now()) }
}

func (a *Autopilot) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case AutopilotLoadedMsg:
		a.data, a.loaded, a.busy = m, true, false
		a.offset = min(a.offset, max(0, len(m.events)-1))
		return nil
	case AutopilotTickMsg:
		return tea.Batch(AutopilotTick(), a.Refresh())
	case tea.KeyPressMsg:
		switch m.String() {
		case "up", "k":
			a.offset = min(max(0, len(a.data.events)-1), a.offset+1)
		case "down", "j":
			a.offset = max(0, a.offset-1)
		case "s":
			if a.busy {
				return nil
			}
			a.busy = true
			paths := a.paths
			return func() tea.Msg {
				var err error
				if paths.Stopped() {
					err = paths.ClearStop()
				} else {
					err = paths.SetStop("manual stop from TUI cockpit", time.Now())
				}
				result := readAutopilot(paths, time.Now())
				if err != nil {
					result.err = "STOP toggle: " + err.Error()
				}
				return result
			}
		}
	}
	return nil
}

// Bound all reads, including the tail of large logs, so polling cannot consume
// unbounded memory. Only complete JSONL records in the tail are displayed.
func autopilotJSON(path string, v any) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("file exceeds 1 MiB")
	}
	return json.Unmarshal(b, v)
}

func readAutopilot(paths runstate.Paths, now time.Time) AutopilotLoadedMsg {
	r := AutopilotLoadedMsg{status: runstate.Status{State: "idle"}, stopped: paths.Stopped()}
	var problems []string
	read := func(label, path string, v any) bool {
		if err := autopilotJSON(path, v); err != nil {
			problems = append(problems, label+": "+err.Error())
			return false
		}
		return true
	}
	read("status", paths.StatusFile(), &r.status)
	var usage runstate.Usage
	if read("usage", paths.UsageFile(), &usage) && usage.Day == now.Format("2006-01-02") {
		r.today = usage.Today
	}
	// usage.json stores consumption; the cap belongs to config.json. Decode
	// only that field and never render or write the rest of the configuration.
	var cfg struct {
		DailyTokens *int64 `json:"daily_tokens"`
	}
	if read("daily cap", filepath.Join(paths.Root, "config.json"), &cfg) && cfg.DailyTokens != nil {
		r.cap, r.capKnown = *cfg.DailyTokens, true
	}
	if r.status.LogPath != "" {
		var err error
		r.events, err = autopilotEvents(paths, r.status.LogPath)
		if err != nil {
			problems = append(problems, "run log: "+err.Error())
		}
	}
	r.err = strings.Join(problems, "; ")
	return r
}

func autopilotEvents(paths runstate.Paths, path string) ([]string, error) {
	root, err := filepath.EvalSymlinks(paths.RunsDir())
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("path is outside runs directory")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	start := max(int64(0), info.Size()-(256<<10))
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, 256<<10))
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(b, []byte{'\n'})
	if start > 0 {
		lines = lines[1:]
	}
	var events []string
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var e runstate.Event
		if json.Unmarshal(line, &e) != nil {
			continue
		} // writer may be appending the last record
		stamp := e.TS
		if ts, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
			stamp = ts.Local().Format("15:04:05")
		}
		events = append(events, autopilotText(strings.TrimSpace(stamp+" "+e.Kind+" "+e.Stage+" "+e.Msg)))
		if len(events) > 20 {
			events = events[1:]
		}
	}
	return events, nil
}

func autopilotText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
}

func (a *Autopilot) View() string {
	if !a.loaded {
		return workFit("Loading autopilot status...", a.w, a.h)
	}
	d := a.data
	stop, action := "clear", "s stop"
	if d.stopped {
		stop, action = "SET", "s resume"
	}
	cap := fmt.Sprint(d.cap)
	if d.cap <= 0 {
		cap = "unlimited"
	}
	if !d.capKnown {
		cap = "unknown"
	}
	card := d.status.Card
	if card == "" {
		card = "No active card"
	}
	rows := []string{
		chrome.PanelHeader.Render("AUTOPILOT  " + autopilotText(d.status.State)),
		"STOP: " + stop + "  |  " + action,
		autopilotText(card + "  " + d.status.Title),
		fmt.Sprintf("Stage: %s  Round: %d", autopilotText(d.status.Stage), d.status.Round),
		fmt.Sprintf("Run tokens: %d", d.status.Tokens),
		fmt.Sprintf("Today: %d / %s tokens", d.today, cap),
	}
	if d.err != "" {
		rows = append(rows, "Read error (retrying): "+autopilotText(d.err))
	} else if d.status.Note != "" {
		rows = append(rows, autopilotText(d.status.Note))
	}
	rows = append(rows, "", "Recent events (last 20)")
	if len(d.events) == 0 {
		rows = append(rows, "No events for the current run.")
	} else {
		end := max(0, len(d.events)-a.offset)
		start := max(0, end-max(1, a.h-len(rows)-1))
		rows = append(rows, d.events[start:end]...)
	}
	// Keep the switch hint available even in a short terminal.
	rows = rows[:min(len(rows), max(0, a.h-1))]
	rows = append(rows, action+"  |  up/down events")
	return workFit(strings.Join(rows, "\n"), a.w, a.h)
}
