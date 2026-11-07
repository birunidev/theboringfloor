package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type rawObject map[string]json.RawMessage
type status struct {
	State     string `json:"state"`
	Run       string `json:"run"`
	Card      string `json:"card"`
	Title     string `json:"title"`
	Stage     string `json:"stage"`
	Note      string `json:"note"`
	UpdatedAt string `json:"updatedAt"`
	LogPath   string `json:"-"`
	Round     *int64 `json:"round"`
	Tokens    *int64 `json:"tokens"`
}
type event struct {
	ID      string    `json:"id"`
	Native  rawObject `json:"native"`
	History bool      `json:"history"`
	// Lane names the workers.json lane whose log this record came from. Empty
	// for the single-worker status.json log.
	Lane string `json:"lane,omitempty"`
}

// laneSnap is one live lane from workers.json plus its own log's records.
type laneSnap struct {
	Lane         string  `json:"lane"`
	Status       *status `json:"status"`
	LogIdentity  string  `json:"logIdentity"`
	Events       []event `json:"events"`
	LogAvailable bool    `json:"logAvailable"`
	LogError     string  `json:"logError"`
	// CardTokens is usage.json's lifetime total for this lane's card (nil when
	// unavailable). The card cap is in snapshot.CardCap.
	CardTokens *int64 `json:"cardTokens"`
}

// workersSnap is workers.json as the office may use it. Usable is false when
// the file is not provably the pair of the current status.json (Reason says
// why); lanes are then not shown, never guessed.
type workersSnap struct {
	Version   int64      `json:"version"`
	UpdatedAt string     `json:"updatedAt"`
	Lanes     []laneSnap `json:"lanes"`
	Usable    bool       `json:"usable"`
	Reason    string     `json:"reason"`
}
type runInfo struct {
	Card     string `json:"card"`
	Run      string `json:"run"`
	Modified string `json:"modified"`
}

// outcome is one real `end` event from a run log written today (server-local
// day). It is a run record, never a claim about current board membership.
type outcome struct {
	Card    string `json:"card"`
	Run     string `json:"run"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	PR      string `json:"pr"`
	Rounds  *int64 `json:"rounds"`
	TS      string `json:"ts"`
	EventID string `json:"eventId"`
}
type snapshot struct {
	Status       *status   `json:"status"`
	StatusError  string    `json:"statusError"`
	Today        *int64    `json:"today"`
	CardTokens   *int64    `json:"cardTokens"`
	DailyCap     *int64    `json:"dailyCap"`
	CardCap      *int64    `json:"cardCap"`
	UsageDay     string    `json:"usageDay"`
	Timezone     string    `json:"timezone"`
	UsageError   string    `json:"usageError"`
	ConfigError  string    `json:"configError"`
	Stopped      *bool     `json:"stopped"`
	StopError    string    `json:"stopError"`
	Boards       []string  `json:"boards"`
	Runs         []runInfo `json:"runs"`
	RunsError    string    `json:"runsError"`
	Events       []event   `json:"events"`
	LogAvailable bool      `json:"logAvailable"`
	LogIdentity  string    `json:"logIdentity"`
	LogError     string    `json:"logError"`
	Controls     bool      `json:"controls"`
	TestData     bool      `json:"testData"`
	// LastRun is the most recently modified run log, offered as static
	// history only while status.json names no current log (idle).
	LastRun      *runInfo `json:"lastRun"`
	LastEvents   []event  `json:"lastEvents"`
	LastRunError string   `json:"lastRunError"`
	// TodayOutcomes are real `end` events from run logs dated today.
	TodayOutcomes []outcome `json:"todayOutcomes"`
	OutcomesDay   string    `json:"outcomesDay"`
	OutcomesError string    `json:"outcomesError"`
	// Workers is nil when workers.json does not exist (no parallel-lane run
	// has been recorded). WorkersError is set when it exists but is unreadable.
	Workers      *workersSnap `json:"workers"`
	WorkersError string       `json:"workersError"`
}

func str(m rawObject, key string) string { var s string; json.Unmarshal(m[key], &s); return s }
func number(m rawObject, key string, nonnegative bool) *int64 {
	var n int64
	b, ok := m[key]
	if !ok || string(b) == "null" || json.Unmarshal(b, &n) != nil || (nonnegative && n < 0) || n > 9007199254740991 || n < -9007199254740991 {
		return nil
	}
	return &n
}
func capValue(m rawObject, key string, defaultValue int64) *int64 {
	if _, ok := m[key]; !ok {
		return &defaultValue
	}
	n := number(m, key, true)
	if n != nil && *n > 0 {
		return n
	}
	return nil
}

// os.Root confines every telemetry read, including symlink resolution, to the
// configured autopilot home. No endpoint accepts a client-selected file path.
func readObject(root *os.Root, path string) (rawObject, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("invalid telemetry file")
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("telemetry too large")
	}
	var obj rawObject
	if err = json.Unmarshal(b, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("malformed JSON object")
	}
	return obj, nil
}

func readStatus(root *os.Root) (*status, error) {
	m, err := readObject(root, "status.json")
	if err != nil {
		return nil, err
	}
	return statusFromObject(m), nil
}

func statusFromObject(m rawObject) *status {
	return &status{State: str(m, "state"), Run: str(m, "run"), Card: str(m, "card"), Title: str(m, "title"), Stage: str(m, "stage"), Note: str(m, "note"), UpdatedAt: str(m, "updatedAt"), LogPath: str(m, "logPath"), Round: number(m, "round", false), Tokens: number(m, "tokens", true)}
}

const (
	maxLanes    = 8
	maxLaneName = 64
	workersFile = "workers.json" // written by internal/autopilot/runstate (WriteWorkers)
)

type laneRaw struct {
	name string
	st   *status
}

// parseWorkers validates workers.json strictly: version 1, a workers array,
// unique non-empty lane names, bounded count.
func parseWorkers(m rawObject) (string, []laneRaw, error) {
	if v := number(m, "version", true); v == nil || *v != 1 {
		return "", nil, fmt.Errorf("unsupported workers.json version")
	}
	var entries []rawObject
	if string(m["workers"]) == "" || string(m["workers"]) == "null" || json.Unmarshal(m["workers"], &entries) != nil {
		return "", nil, fmt.Errorf("workers is not an array")
	}
	if len(entries) > maxLanes {
		return "", nil, fmt.Errorf("too many lanes")
	}
	seen := map[string]bool{}
	var lanes []laneRaw
	for _, e := range entries {
		name := str(e, "lane")
		if name == "" || len(name) > maxLaneName || seen[name] {
			return "", nil, fmt.Errorf("invalid or duplicate lane name")
		}
		seen[name] = true
		lanes = append(lanes, laneRaw{name: name, st: statusFromObject(e)})
	}
	return str(m, "updatedAt"), lanes, nil
}

// workersUsable decides whether workers.json is the pair of the current
// status.json. The engine writes both in one call with the same timestamp
// (Engine.persistStatusesLocked), so equal times prove they belong together.
// A single-worker run writes only status.json, which makes an older
// workers.json stale; a crash can leave one behind. Neither may be shown.
func workersUsable(updatedAt string, lanes []laneRaw, st *status) (bool, string) {
	if st == nil {
		return false, "status.json is unavailable, so lane data cannot be confirmed"
	}
	wt, err1 := time.Parse(time.RFC3339, updatedAt)
	stt, err2 := time.Parse(time.RFC3339, st.UpdatedAt)
	if err1 != nil || err2 != nil {
		return false, "update time unavailable in workers.json or status.json"
	}
	switch {
	case wt.Before(stt):
		return false, "workers.json is older than status.json (a later single-worker write); lane data ignored"
	case wt.After(stt):
		return false, "workers.json is newer than status.json (unpaired write); lane data ignored"
	}
	switch {
	case len(lanes) == 0:
		if st.State != "idle" {
			return false, "workers.json lists no lanes but status.json is not idle"
		}
	case len(lanes) == 1:
		l := lanes[0].st
		if st.State != l.State || st.Run != l.Run || st.Card != l.Card {
			return false, "status.json does not mirror the single lane in workers.json"
		}
	default:
		if st.State != "running" || st.Card != "" {
			return false, "status.json is not the multi-lane summary for workers.json"
		}
	}
	return true, ""
}

// readWorkers loads workers.json and, when it is usable, each lane's own log.
func (o *office) readWorkers(root *os.Root, st *status) (*workersSnap, string) {
	m, err := readObject(root, workersFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ""
	}
	if err != nil {
		return nil, "Lane data unavailable"
	}
	updatedAt, lanes, err := parseWorkers(m)
	if err != nil {
		return nil, "Lane data unavailable"
	}
	w := &workersSnap{Version: 1, UpdatedAt: updatedAt, Lanes: []laneSnap{}}
	w.Usable, w.Reason = workersUsable(updatedAt, lanes, st)
	if !w.Usable {
		return w, ""
	}
	for _, l := range lanes {
		ls := laneSnap{Lane: l.name, Status: l.st, Events: []event{}}
		if l.st.LogPath != "" {
			f, identity, err := o.openLog(root, l.st.LogPath)
			if err != nil {
				ls.LogError = "Activity unavailable"
			} else {
				ls.LogIdentity = identity
				evs, err := tailEvents(f, identity, 500)
				f.Close()
				if err != nil {
					ls.LogError = "Activity unavailable"
				} else {
					ls.LogAvailable = true
					for i := range evs {
						evs[i].Lane = l.name
					}
					ls.Events = evs
				}
			}
		}
		w.Lanes = append(w.Lanes, ls)
	}
	return w, ""
}

// logSource is one run log the event stream follows.
type logSource struct {
	Lane string
	Path string
}

// logSources lists the logs to follow: the status.json log plus every lane
// log of a usable workers.json. A lane log that is also the status log is
// followed once, labelled with its lane.
func (o *office) logSources(root *os.Root, st *status) []logSource {
	var out []logSource
	seen := map[string]bool{}
	if m, err := readObject(root, workersFile); err == nil {
		if updatedAt, lanes, err := parseWorkers(m); err == nil {
			if ok, _ := workersUsable(updatedAt, lanes, st); ok {
				for _, l := range lanes {
					if l.st.LogPath != "" && !seen[l.st.LogPath] {
						seen[l.st.LogPath] = true
						out = append(out, logSource{Lane: l.name, Path: l.st.LogPath})
					}
				}
			}
		}
	}
	if st != nil && st.LogPath != "" && !seen[st.LogPath] {
		out = append(out, logSource{Path: st.LogPath})
	}
	return out
}

func (o *office) snapshot() snapshot {
	s := snapshot{Controls: o.controls, TestData: o.testData, Boards: []string{}, Runs: []runInfo{}, Events: []event{}, LastEvents: []event{}, TodayOutcomes: []outcome{}}
	now := o.now()
	zone, _ := now.Zone()
	s.Timezone = zone
	root, err := os.OpenRoot(o.home)
	if err != nil {
		s.StatusError = "Waiting for status"
		s.UsageError = "Usage unavailable"
		s.StopError = "Stop state unavailable"
		s.ConfigError = "Configuration unavailable"
		s.RunsError = "Recent runs unavailable"
		return s
	}
	defer root.Close()
	s.Status, err = readStatus(root)
	if err != nil {
		s.StatusError = "Status unavailable"
		if errors.Is(err, os.ErrNotExist) {
			s.StatusError = "Waiting for status"
		}
	}
	if _, err := root.Stat("STOP"); err == nil {
		b := true
		s.Stopped = &b
	} else if errors.Is(err, os.ErrNotExist) {
		b := false
		s.Stopped = &b
	} else {
		s.StopError = "Stop state unavailable"
	}
	cfg, err := readObject(root, "config.json")
	if err == nil || errors.Is(err, os.ErrNotExist) {
		s.DailyCap = capValue(cfg, "daily_tokens", 10000000)
		s.CardCap = capValue(cfg, "per_card_tokens", 2000000)
		var boards []rawObject
		if json.Unmarshal(cfg["boards"], &boards) == nil {
			for _, b := range boards {
				if name := str(b, "name"); name != "" {
					s.Boards = append(s.Boards, name)
				}
			}
		}
	} else {
		s.ConfigError = "Configuration unavailable"
	}
	var usageCards rawObject
	u, err := readObject(root, "usage.json")
	if err != nil {
		s.UsageError = "Usage unavailable"
	} else {
		s.UsageDay = str(u, "day")
		if day, err := time.Parse("2006-01-02", s.UsageDay); err == nil && day.Format("2006-01-02") == s.UsageDay {
			s.Today = number(u, "today", true)
			// Mirror Budget.Snapshot's server-local day rollover without
			// opening a writable Budget or changing the ledger.
			if s.Today != nil && s.UsageDay < now.Format("2006-01-02") {
				n := int64(0)
				s.Today = &n
			}
			if s.UsageDay > now.Format("2006-01-02") {
				s.Today = nil
			}
		}
		var cards rawObject
		if json.Unmarshal(u["cards"], &cards) == nil {
			usageCards = cards
		}
		if s.Status != nil && s.Status.State == "running" && s.Status.Card != "" && usageCards != nil {
			s.CardTokens = number(usageCards, s.Status.Card, true)
		}
	}
	s.Runs, err = recentRuns(root)
	if err != nil {
		s.RunsError = "Recent runs unavailable"
	}
	s.Workers, s.WorkersError = o.readWorkers(root, s.Status)
	if s.Workers != nil && usageCards != nil {
		for i := range s.Workers.Lanes {
			if card := s.Workers.Lanes[i].Status.Card; card != "" {
				s.Workers.Lanes[i].CardTokens = number(usageCards, card, true)
			}
		}
	}
	all, allErr := allRuns(root)
	s.OutcomesDay = now.Format("2006-01-02")
	if allErr != nil {
		s.OutcomesError = "Run logs unavailable"
	} else {
		s.TodayOutcomes, s.OutcomesError = o.todayOutcomes(root, all, now)
	}
	// Finished-run history is for an idle office. While status.json says
	// running (one worker without a log path, or several lanes) it would be
	// presented as "finished" while work is live, so it is not offered.
	if (s.Status == nil || (s.Status.LogPath == "" && s.Status.State != "running")) && allErr == nil && len(all) > 0 {
		last := all[0]
		s.LastRun = &last
		f, identity, err := o.openLog(root, filepath.Join("runs", last.Card, last.Run+".jsonl"))
		if err != nil {
			s.LastRunError = "Last run unavailable"
		} else {
			s.LastEvents, err = tailEvents(f, identity, 500)
			f.Close()
			if err != nil {
				s.LastRunError = "Last run unavailable"
			}
			for i := range s.LastEvents {
				s.LastEvents[i].History = true
			}
		}
	}
	if s.Status != nil && s.Status.LogPath != "" {
		f, identity, err := o.openLog(root, s.Status.LogPath)
		if err != nil {
			s.LogError = "Activity unavailable"
		} else {
			s.LogIdentity = identity
			offset := int64(0)
			if info, statErr := f.Stat(); statErr == nil && info.Size() > 4<<20 {
				offset = info.Size() - (4 << 20)
				f.Seek(offset, io.SeekStart)
				prefix, _ := bufio.NewReader(f).ReadBytes('\n')
				offset += int64(len(prefix))
			}
			s.Events, _, err = readEvents(f, identity, offset, 500)
			f.Close()
			if err != nil {
				s.LogError = "Activity unavailable"
			} else {
				s.LogAvailable = true
			}
		}
	}
	return s
}

func recentRuns(root *os.Root) ([]runInfo, error) {
	list, err := allRuns(root)
	return list[:min(20, len(list))], err
}

// maxRunFiles bounds how many run logs one snapshot inspects.
const maxRunFiles = 2000

// allRuns lists run logs (newest first) under runs/<card>/<run>.jsonl,
// skipping symlinks and non-regular files. Reads stay inside the root.
func allRuns(root *os.Root) ([]runInfo, error) {
	list := []runInfo{}
	f, err := root.Open("runs")
	if errors.Is(err, os.ErrNotExist) {
		return list, nil
	}
	if err != nil {
		return list, err
	}
	dirs, err := f.ReadDir(1000)
	f.Close()
	if err != nil && err != io.EOF {
		return list, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Type()&os.ModeSymlink != 0 {
			continue
		}
		d, err := root.Open(filepath.Join("runs", dir.Name()))
		if err != nil {
			continue
		}
		files, _ := d.ReadDir(1000)
		d.Close()
		for _, file := range files {
			if file.Type().IsRegular() && strings.HasSuffix(file.Name(), ".jsonl") {
				info, err := file.Info()
				if err == nil {
					list = append(list, runInfo{Card: dir.Name(), Run: strings.TrimSuffix(file.Name(), ".jsonl"), Modified: info.ModTime().UTC().Format(time.RFC3339)})
				}
			}
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Modified > list[j].Modified })
	return list[:min(maxRunFiles, len(list))], nil
}

// tailEvents reads up to limit records from the last 4 MiB of a log.
func tailEvents(f *os.File, identity string, limit int) ([]event, error) {
	offset := int64(0)
	if info, err := f.Stat(); err == nil && info.Size() > 4<<20 {
		offset = info.Size() - (4 << 20)
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
		prefix, _ := bufio.NewReader(f).ReadBytes('\n')
		offset += int64(len(prefix))
	}
	evs, _, err := readEvents(f, identity, offset, limit)
	return evs, err
}

// todayOutcomes collects real `end` events dated today (server-local) from
// run logs modified today. Unreadable logs are skipped and reported once.
func (o *office) todayOutcomes(root *os.Root, runs []runInfo, now time.Time) ([]outcome, string) {
	day := now.Format("2006-01-02")
	out := []outcome{}
	seen := map[string]bool{}
	failed := false
	for _, r := range runs {
		mod, err := time.Parse(time.RFC3339, r.Modified)
		if err != nil || mod.In(now.Location()).Format("2006-01-02") != day {
			continue
		}
		f, identity, err := o.openLog(root, filepath.Join("runs", r.Card, r.Run+".jsonl"))
		if err != nil {
			failed = true
			continue
		}
		evs, err := tailEvents(f, identity, 500)
		f.Close()
		if err != nil {
			failed = true
		}
		for _, e := range evs {
			if str(e.Native, "kind") != "end" {
				continue
			}
			var data rawObject
			if json.Unmarshal(e.Native["data"], &data) != nil {
				continue
			}
			kind := str(data, "kind")
			if kind != "ready-for-review" && kind != "needs-clarification" && kind != "blocked" {
				continue
			}
			ts := str(e.Native, "ts")
			t, err := time.Parse(time.RFC3339Nano, ts)
			if err != nil || t.In(now.Location()).Format("2006-01-02") != day {
				continue
			}
			card, run := str(e.Native, "card"), str(e.Native, "run")
			if card == "" || run == "" || seen[run+":"+card] {
				continue
			}
			seen[run+":"+card] = true
			out = append(out, outcome{Card: card, Run: run, Kind: kind, Summary: str(data, "summary"),
				PR: str(data, "pr"), Rounds: number(data, "rounds", true), TS: ts, EventID: e.ID})
		}
		if len(out) >= 200 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	if failed {
		return out, "Some run logs were unreadable"
	}
	return out, ""
}

func (o *office) openLog(root *os.Root, path string) (*os.File, string, error) {
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(o.home, path)
		if err != nil {
			return nil, "", err
		}
	}
	path = filepath.Clean(path)
	if !strings.HasPrefix(path, "runs"+string(os.PathSeparator)) || !strings.HasSuffix(path, ".jsonl") {
		return nil, "", fmt.Errorf("invalid run path")
	}
	runsRoot, err := root.OpenRoot("runs")
	if err != nil {
		return nil, "", err
	}
	defer runsRoot.Close()
	f, err := runsRoot.Open(strings.TrimPrefix(path, "runs"+string(os.PathSeparator)))
	if err != nil {
		return nil, "", err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, "", fmt.Errorf("invalid run file")
	}
	identity := path
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity += fmt.Sprintf(":%d:%d", stat.Dev, stat.Ino)
	}
	hash := sha256.Sum256([]byte(identity))
	return f, hex.EncodeToString(hash[:8]), nil
}

// Incomplete trailing records remain unread until a newline arrives. Stable
// file-identity/offset IDs let reconnecting clients deduplicate real records.
func readEvents(f *os.File, identity string, offset int64, limit int) ([]event, int64, error) {
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	scan := bufio.NewReader(io.LimitReader(f, 4<<20))
	out := []event{}
	for {
		line, err := scan.ReadBytes('\n')
		if len(line) > 1<<20 {
			return out, offset, fmt.Errorf("event too large")
		}
		if err == io.EOF {
			return out, offset, nil
		}
		if err != nil {
			return out, offset, err
		}
		start := offset
		offset += int64(len(line))
		var native rawObject
		if json.Unmarshal(line, &native) != nil || native == nil {
			return out, offset, fmt.Errorf("malformed event")
		}
		hash := sha256.Sum256(line)
		out = append(out, event{ID: fmt.Sprintf("%s:%d:%x", identity, start, hash[:6]), Native: native})
		if len(out) > limit {
			out = out[1:]
		}
	}
}
