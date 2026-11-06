package runstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is one JSONL line in a run log.
type Event struct {
	TS    string         `json:"ts"`
	Run   string         `json:"run"`
	Card  string         `json:"card"`
	Stage string         `json:"stage,omitempty"`
	Round int            `json:"round,omitempty"`
	Kind  string         `json:"kind"` // start, stage, verdict, tokens, board, error, end
	Msg   string         `json:"msg,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// Status is the live state the TUI cockpit reads from status.json.
type Status struct {
	State     string `json:"state"` // idle, running, stopped, capped
	Run       string `json:"run,omitempty"`
	Card      string `json:"card,omitempty"`
	Title     string `json:"title,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Round     int    `json:"round,omitempty"`
	Tokens    int64  `json:"tokens,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	UpdatedAt string `json:"updatedAt"`
	Note      string `json:"note,omitempty"`
	LogPath   string `json:"logPath,omitempty"`
}

// RunLog appends events for one run to runs/<card>/<run>.jsonl.
type RunLog struct {
	mu   sync.Mutex
	f    *os.File
	path string
	run  string
	card string
	now  func() time.Time
}

// NewRunID returns a sortable run id.
func NewRunID(now time.Time) string {
	return now.UTC().Format("20060102T150405.000Z")
}

// OpenRunLog creates the log file for one run.
func (p Paths) OpenRunLog(card, run string, now func() time.Time) (*RunLog, error) {
	if now == nil {
		now = time.Now
	}
	dir := filepath.Join(p.RunsDir(), SafeName(card))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, SafeName(run)+".jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &RunLog{f: f, path: path, run: run, card: card, now: now}, nil
}

// Path is the log file path.
func (l *RunLog) Path() string { return l.path }

// Write appends one event; Run, Card and TS are filled in.
func (l *RunLog) Write(e Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.TS = l.now().UTC().Format(time.RFC3339Nano)
	e.Run, e.Card = l.run, l.card
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("run log write: %w", err)
	}
	return nil
}

// Close closes the log file.
func (l *RunLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

// WriteStatus atomically replaces status.json.
func (p Paths) WriteStatus(s Status, now time.Time) error {
	s.UpdatedAt = now.UTC().Format(time.RFC3339)
	return writeJSONAtomic(p.StatusFile(), s)
}

// ReadStatus reads status.json (zero Status when absent).
func (p Paths) ReadStatus() (Status, error) {
	var s Status
	data, err := os.ReadFile(p.StatusFile())
	if os.IsNotExist(err) {
		return Status{State: "idle"}, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}
