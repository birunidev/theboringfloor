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
}
type runInfo struct {
	Card     string `json:"card"`
	Run      string `json:"run"`
	Modified string `json:"modified"`
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
	return &status{State: str(m, "state"), Run: str(m, "run"), Card: str(m, "card"), Title: str(m, "title"), Stage: str(m, "stage"), Note: str(m, "note"), UpdatedAt: str(m, "updatedAt"), LogPath: str(m, "logPath"), Round: number(m, "round", false), Tokens: number(m, "tokens", true)}, nil
}

func (o *office) snapshot() snapshot {
	s := snapshot{Controls: o.controls, TestData: o.testData, Boards: []string{}, Runs: []runInfo{}, Events: []event{}}
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
		if s.Status != nil && s.Status.State == "running" && s.Status.Card != "" {
			var cards rawObject
			if json.Unmarshal(u["cards"], &cards) == nil {
				s.CardTokens = number(cards, s.Status.Card, true)
			}
		}
	}
	s.Runs, err = recentRuns(root)
	if err != nil {
		s.RunsError = "Recent runs unavailable"
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
	return list[:min(20, len(list))], nil
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
