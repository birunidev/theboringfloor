package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, home, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(home, path)
	if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) (*office, *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	srv := httptest.NewUnstartedServer(nil)
	o, err := newOffice(home, "http://"+srv.Listener.Addr().String(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	o.poll = 10 * time.Millisecond
	o.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	srv.Config.Handler = o
	srv.Start()
	t.Cleanup(srv.Close)
	return o, srv
}
func getSnapshot(t *testing.T, srv *httptest.Server) snapshot {
	t.Helper()
	res, err := http.Get(srv.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.Status)
	}
	var s snapshot
	if err = json.NewDecoder(res.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStateMergeAndUnknownValues(t *testing.T) {
	o, srv := fixture(t)
	put(t, o.home, "status.json", map[string]any{"state": "running", "card": "BII-812", "run": "r1", "stage": "implement:2", "round": 2, "tokens": 42, "title": "<script>test</script>", "logPath": filepath.Join(o.home, "runs/BII-812/r1.jsonl")})
	put(t, o.home, "usage.json", map[string]any{"day": "2026-11-07", "today": 800, "cards": map[string]int{"BII-812": 90, "BII-811": 333}})
	put(t, o.home, "config.json", map[string]any{"daily_tokens": 1000, "per_card_tokens": 100, "boards": []any{map[string]string{"name": "V2 Engineering"}}, "secret": "do not expose"})
	put(t, o.home, "runs/BII-812/r1.jsonl", map[string]any{"kind": "stage-done", "run": "r1", "card": "BII-812", "stage": "implement:2"})
	s := getSnapshot(t, srv)
	if s.Status.Card != "BII-812" || *s.Today != 800 || *s.CardTokens != 90 || *s.DailyCap != 1000 || *s.CardCap != 100 || *s.Stopped || len(s.Boards) != 1 || len(s.Runs) != 1 || !s.LogAvailable || len(s.Events) != 1 {
		t.Fatalf("wrong merge: %+v", s)
	}
	res, _ := http.Get(srv.URL + "/api/state")
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(body), "do not expose") || strings.Contains(string(body), "<script>") {
		t.Fatal("unsafe JSON representation")
	}
	put(t, o.home, "usage.json", map[string]any{"day": "2026-11-06", "today": 800, "cards": map[string]int{"BII-812": 90}})
	s = getSnapshot(t, srv)
	if s.Today == nil || *s.Today != 0 {
		t.Fatal("server-local day did not roll")
	}
	put(t, o.home, "usage.json", map[string]any{"day": "2026-11-07", "today": -1, "cards": map[string]any{"BII-812": "90"}})
	put(t, o.home, "config.json", map[string]any{"daily_tokens": 0, "per_card_tokens": "invalid"})
	s = getSnapshot(t, srv)
	if s.Today != nil || s.CardTokens != nil || s.DailyCap != nil || s.CardCap != nil {
		t.Fatal("invalid values fabricated as numbers")
	}
	os.Remove(filepath.Join(o.home, "config.json"))
	s = getSnapshot(t, srv)
	if *s.DailyCap != 10000000 || *s.CardCap != 2000000 {
		t.Fatal("specified defaults missing")
	}
	put(t, o.home, "status.json", map[string]any{"state": "idle", "card": "BII-812", "stage": "implement:2"})
	s = getSnapshot(t, srv)
	if s.CardTokens != nil {
		t.Fatal("idle retained card meter")
	}
	os.WriteFile(filepath.Join(o.home, "status.json"), []byte("{"), 0600)
	s = getSnapshot(t, srv)
	if s.Status != nil || s.StatusError != "Status unavailable" {
		t.Fatal("malformed status not explicit")
	}
}

func TestControlTokenOriginAndReadOnly(t *testing.T) {
	o, srv := fixture(t)
	for _, test := range []struct {
		method, origin, token string
		want                  int
	}{{"POST", "", "", 403}, {"POST", srv.URL, "wrong", 403}, {"POST", "http://attacker.test", o.token, 403}, {"GET", srv.URL, o.token, 405}, {"POST", srv.URL, o.token, 200}} {
		req, _ := http.NewRequest(test.method, srv.URL+"/api/stop", nil)
		req.Header.Set("Origin", test.origin)
		req.Header.Set("X-Office-Token", test.token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != test.want {
			t.Fatalf("control status %d, want %d", res.StatusCode, test.want)
		}
		_, err = os.Stat(filepath.Join(o.home, "STOP"))
		if (err == nil) != (test.want == 200) {
			t.Fatal("unauthorized mutation")
		}
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/resume", nil)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("X-Office-Token", o.token)
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.Status)
	}
	if _, err := os.Stat(filepath.Join(o.home, "STOP")); !os.IsNotExist(err) {
		t.Fatal("resume did not clear")
	}
	o.controls = false
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("readonly controls active")
	}
	o.controls = true
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("preserved"), 0600)
	os.Symlink(outside, filepath.Join(o.home, "STOP"))
	req, _ = http.NewRequest("POST", srv.URL+"/api/stop", nil)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("X-Office-Token", o.token)
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	b, _ := os.ReadFile(outside)
	if res.StatusCode != 409 || string(b) != "preserved" {
		t.Fatal("STOP symlink followed")
	}
	other, err := newOffice(o.home, srv.URL, true, false)
	if err != nil || other.token == o.token {
		t.Fatal("token not process-unique")
	}
}

func TestBindAndHostChecks(t *testing.T) {
	for _, address := range []string{":0", "0.0.0.0:0", "localhost:0", "[::1]:0", "192.168.0.1:0"} {
		if ln, err := listen(address); err == nil {
			ln.Close()
			t.Fatalf("accepted %s", address)
		}
	}
	ln, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	_, srv := fixture(t)
	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	req.Host = "attacker.test"
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("DNS rebinding host accepted")
	}
}

func TestTraversalAndSymlinksRefused(t *testing.T) {
	o, srv := fixture(t)
	outside := filepath.Join(t.TempDir(), "private.jsonl")
	os.WriteFile(outside, []byte("{\"secret\":\"outside\"}\n"), 0600)
	os.MkdirAll(filepath.Join(o.home, "runs"), 0700)
	os.Symlink(outside, filepath.Join(o.home, "runs", "escape.jsonl"))
	put(t, o.home, "config.json", map[string]string{"secret": "config"})
	os.Symlink("../config.json", filepath.Join(o.home, "runs", "config.jsonl"))
	for _, path := range []string{outside, "../private.jsonl", "runs/../../private.jsonl", "runs/escape.jsonl", "runs/config.jsonl", "config.json"} {
		put(t, o.home, "status.json", map[string]string{"state": "running", "logPath": path})
		s := getSnapshot(t, srv)
		if s.LogAvailable || s.LogError == "" || len(s.Events) != 0 {
			t.Fatalf("log escape accepted: %s", path)
		}
	}
	for _, path := range []string{"/../config.json", "/%2e%2e/config.json", "/assets/%2e%2e/config.json", "/api/state?path=../../private.jsonl", "/runs/escape.jsonl"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if strings.Contains(string(b), "outside") || strings.Contains(string(b), `"secret"`) {
			t.Fatal("raw files served")
		}
		if !strings.HasPrefix(path, "/api/state?") && res.StatusCode == 200 {
			t.Fatalf("unexpected route: %s", path)
		}
	}
}

func TestSSETailAcrossRotationAndPartialLines(t *testing.T) {
	o, srv := fixture(t)
	path := "runs/BII-812/one.jsonl"
	put(t, o.home, path, map[string]string{"kind": "start", "run": "one", "card": "BII-812"})
	put(t, o.home, "status.json", map[string]string{"state": "running", "logPath": path})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rows := make(chan event, 20)
	go func() {
		defer close(rows)
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				var e event
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e) == nil && e.ID != "" {
					rows <- e
				}
			}
		}
	}()
	next := func(want string) event {
		t.Helper()
		select {
		case row := <-rows:
			if str(row.Native, "kind") != want {
				t.Fatalf("got %s, want %s", str(row.Native, "kind"), want)
			}
			return row
		case <-time.After(2 * time.Second):
			t.Fatal("SSE timeout")
			return event{}
		}
	}
	first := next("start")
	f, _ := os.OpenFile(filepath.Join(o.home, path), os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprint(f, `{"kind":"permission","data":{"allow":false}}`)
	f.Close()
	select {
	case <-rows:
		t.Fatal("partial event emitted")
	case <-time.After(35 * time.Millisecond):
	}
	f, _ = os.OpenFile(filepath.Join(o.home, path), os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprint(f, "\n")
	f.Close()
	second := next("permission")
	if first.ID == second.ID {
		t.Fatal("offset IDs collide")
	}
	newPath := "runs/BII-999/two.jsonl"
	put(t, o.home, newPath, map[string]string{"kind": "end", "run": "two", "card": "BII-999"})
	put(t, o.home, "status.json", map[string]string{"state": "running", "logPath": newPath})
	third := next("end")
	if third.ID == first.ID {
		t.Fatal("rotation IDs collide")
	}
	os.Rename(filepath.Join(o.home, newPath), filepath.Join(o.home, newPath+".old"))
	put(t, o.home, newPath, map[string]string{"kind": "error", "run": "two", "card": "BII-999"})
	next("error")
}

func TestEmbeddedAssetsAndNoReadSideEffects(t *testing.T) {
	o, srv := fixture(t)
	for _, path := range []string{"/", "/assets/app.js", "/assets/style.css", "/api/state"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%s: %s", path, res.Status)
		}
		if path == "/" && !strings.Contains(string(b), o.token) {
			t.Fatal("process token not embedded")
		}
		if path == "/assets/app.js" && strings.Contains(string(b), "innerHTML") {
			t.Fatal("unsafe DOM sink")
		}
		if res.Header.Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
	entries, _ := os.ReadDir(o.home)
	if len(entries) != 0 {
		t.Fatal("GET wrote files")
	}
}

// writeRun writes JSONL records to runs/<card>/<run>.jsonl with a fixed mtime.
func writeRun(t *testing.T, home, card, run string, mod time.Time, records ...any) string {
	t.Helper()
	dir := filepath.Join(home, "runs", card)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, r := range records {
		if s, ok := r.(string); ok {
			b.WriteString(s + "\n")
			continue
		}
		line, _ := json.Marshal(r)
		b.Write(line)
		b.WriteByte('\n')
	}
	name := filepath.Join(dir, run+".jsonl")
	if err := os.WriteFile(name, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, mod, mod); err != nil {
		t.Fatal(err)
	}
	return name
}

func end(card, run, kind, ts string, extra map[string]any) map[string]any {
	data := map[string]any{"kind": kind, "summary": kind + " summary"}
	for k, v := range extra {
		data[k] = v
	}
	return map[string]any{"kind": "end", "card": card, "run": run, "ts": ts, "msg": "done", "data": data}
}

func TestIdleShowsLastRunAndTodayOutcomes(t *testing.T) {
	o, srv := fixture(t)
	day := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	put(t, o.home, "status.json", map[string]any{"state": "idle", "updatedAt": "2026-11-07T11:59:00Z", "note": "last: BII-3 needs-clarification"})
	writeRun(t, o.home, "BII-1", "r-yesterday", day.Add(-2*time.Hour),
		end("BII-1", "r-yesterday", "ready-for-review", "2026-11-06T22:00:00Z", map[string]any{"pr": "https://github.com/o/r/pull/1"}))
	writeRun(t, o.home, "BII-2", "r-blocked", day.Add(9*time.Hour),
		map[string]any{"kind": "start", "card": "BII-2", "run": "r-blocked", "ts": "2026-11-07T08:59:00Z"},
		end("BII-2", "r-blocked", "blocked", "2026-11-07T09:00:00Z", map[string]any{"rounds": 3}))
	writeRun(t, o.home, "BII-3", "r-clarify", day.Add(11*time.Hour),
		map[string]any{"kind": "start", "card": "BII-3", "run": "r-clarify", "ts": "2026-11-07T10:59:00Z"},
		end("BII-3", "r-clarify", "needs-clarification", "2026-11-07T11:00:00Z", nil),
		end("BII-3", "r-clarify", "needs-clarification", "2026-11-07T11:00:01Z", nil)) // duplicate end
	writeRun(t, o.home, "BII-4", "r-dry-dry", day.Add(10*time.Hour),
		map[string]any{"kind": "end", "card": "BII-4", "run": "r-dry-dry", "ts": "2026-11-07T10:00:00Z", "data": map[string]any{"ready": false}})
	writeRun(t, o.home, "BII-5", "r-broken", day.Add(8*time.Hour), "{not json")

	s := getSnapshot(t, srv)
	if s.LastRun == nil || s.LastRun.Card != "BII-3" || s.LastRun.Run != "r-clarify" {
		t.Fatalf("last run = %+v, want the most recently modified log (BII-3)", s.LastRun)
	}
	if len(s.LastEvents) != 3 || !s.LastEvents[0].History {
		t.Fatalf("last events = %d (history=%v), want 3 history records", len(s.LastEvents), len(s.LastEvents) > 0 && s.LastEvents[0].History)
	}
	if len(s.TodayOutcomes) != 2 {
		t.Fatalf("today outcomes = %+v, want BII-2 blocked and BII-3 clarification only", s.TodayOutcomes)
	}
	if got := s.TodayOutcomes[0]; got.Card != "BII-2" || got.Kind != "blocked" || got.Rounds == nil || *got.Rounds != 3 || got.Summary != "blocked summary" {
		t.Fatalf("first outcome = %+v", got)
	}
	if s.TodayOutcomes[1].Card != "BII-3" || s.TodayOutcomes[1].Kind != "needs-clarification" {
		t.Fatalf("second outcome = %+v", s.TodayOutcomes[1])
	}
	if s.OutcomesDay != "2026-11-07" || s.OutcomesError != "Some run logs were unreadable" {
		t.Fatalf("day=%q error=%q", s.OutcomesDay, s.OutcomesError)
	}
}

func TestRunningHasNoLastRunHistory(t *testing.T) {
	o, srv := fixture(t)
	log := writeRun(t, o.home, "BII-9", "r-now", time.Date(2026, 11, 7, 11, 0, 0, 0, time.UTC),
		map[string]any{"kind": "start", "card": "BII-9", "run": "r-now", "ts": "2026-11-07T11:00:00Z"})
	put(t, o.home, "status.json", map[string]any{"state": "running", "run": "r-now", "card": "BII-9", "stage": "preflight", "updatedAt": "2026-11-07T11:59:30Z", "logPath": log})
	s := getSnapshot(t, srv)
	if s.LastRun != nil || len(s.LastEvents) != 0 {
		t.Fatalf("running status must not get last-run history: %+v", s.LastRun)
	}
	if !s.LogAvailable || len(s.Events) != 1 {
		t.Fatalf("current log not served: available=%v events=%d", s.LogAvailable, len(s.Events))
	}
}

func TestRunScanSkipsSymlinks(t *testing.T) {
	o, srv := fixture(t)
	outside := t.TempDir()
	writeRun(t, outside, "BII-X", "r-outside", time.Date(2026, 11, 7, 11, 30, 0, 0, time.UTC),
		end("BII-X", "r-outside", "ready-for-review", "2026-11-07T11:30:00Z", nil))
	if err := os.MkdirAll(filepath.Join(o.home, "runs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "runs", "BII-X"), filepath.Join(o.home, "runs", "BII-X")); err != nil {
		t.Fatal(err)
	}
	put(t, o.home, "status.json", map[string]any{"state": "idle", "updatedAt": "2026-11-07T11:59:00Z"})
	s := getSnapshot(t, srv)
	if s.LastRun != nil || len(s.TodayOutcomes) != 0 {
		t.Fatalf("a symlinked run directory outside the home was read: last=%+v outcomes=%+v", s.LastRun, s.TodayOutcomes)
	}
}

// writeLanes writes workers.json and status.json the way
// Engine.persistStatusesLocked does: one timestamp for both, and status.json
// is the mirror (one lane) or the "N lanes active" summary (several).
func writeLanes(t *testing.T, home, stamp string, lanes ...map[string]any) {
	t.Helper()
	workers := []map[string]any{}
	for _, l := range lanes {
		l["updatedAt"] = stamp
		workers = append(workers, l)
	}
	put(t, home, "workers.json", map[string]any{"version": 1, "updatedAt": stamp, "workers": workers})
	switch len(lanes) {
	case 0:
		put(t, home, "status.json", map[string]any{"state": "idle", "updatedAt": stamp, "note": "last: BII-1 ready-for-review"})
	case 1:
		st := map[string]any{}
		for k, v := range lanes[0] {
			if k != "lane" {
				st[k] = v
			}
		}
		put(t, home, "status.json", st)
	default:
		put(t, home, "status.json", map[string]any{"state": "running", "updatedAt": stamp, "note": fmt.Sprintf("%d lanes active; see workers.json", len(lanes))})
	}
}

func permRecord(stage, action string, allow bool, msg, ts string) map[string]any {
	return map[string]any{"kind": "permission", "stage": stage, "ts": ts, "msg": msg, "data": map[string]any{"action": action, "allow": allow}}
}

func lane(name, run, card, stage, logPath string) map[string]any {
	return map[string]any{"lane": name, "state": "running", "run": run, "card": card, "title": "TEST DATA " + name, "stage": stage, "tokens": 1234, "logPath": logPath}
}

func TestNoWorkersFileMeansNoLaneData(t *testing.T) {
	o, srv := fixture(t)
	put(t, o.home, "status.json", map[string]any{"state": "idle", "updatedAt": "2026-11-07T11:59:00Z"})
	s := getSnapshot(t, srv)
	if s.Workers != nil || s.WorkersError != "" {
		t.Fatalf("absent workers.json must mean no lane data and no error: %+v %q", s.Workers, s.WorkersError)
	}
}

func TestParallelLanesAreServedWithTheirOwnLogs(t *testing.T) {
	o, srv := fixture(t)
	const stamp = "2026-11-07T11:59:50Z"
	fe := writeRun(t, o.home, "BII-833", "r-fe", time.Date(2026, 11, 7, 11, 59, 0, 0, time.UTC),
		map[string]any{"kind": "stage", "stage": "preflight", "ts": "2026-11-07T11:59:01Z", "msg": "session fe"},
		permRecord("preflight", "read", true, "read inside worktree", "2026-11-07T11:59:02Z"))
	be := writeRun(t, o.home, "BII-830", "r-be", time.Date(2026, 11, 7, 11, 59, 0, 0, time.UTC),
		map[string]any{"kind": "stage", "stage": "implement:1", "ts": "2026-11-07T11:59:03Z", "msg": "session be"})
	writeLanes(t, o.home, stamp,
		lane("backend", "r-be", "BII-830", "implement:1", be),
		lane("frontend", "r-fe", "BII-833", "preflight", fe))

	s := getSnapshot(t, srv)
	if s.Workers == nil || !s.Workers.Usable || len(s.Workers.Lanes) != 2 {
		t.Fatalf("workers = %+v", s.Workers)
	}
	b, f := s.Workers.Lanes[0], s.Workers.Lanes[1]
	if b.Lane != "backend" || f.Lane != "frontend" || b.Status.Card != "BII-830" || f.Status.Stage != "preflight" {
		t.Fatalf("lanes = %+v / %+v", b, f)
	}
	if !b.LogAvailable || len(b.Events) != 1 || b.Events[0].Lane != "backend" || b.LogIdentity == "" {
		t.Fatalf("backend lane log = %+v", b)
	}
	if len(f.Events) != 2 || f.Events[1].Lane != "frontend" || f.LogIdentity == b.LogIdentity {
		t.Fatalf("frontend lane log = %+v", f)
	}
	// The summary status.json carries no card: nothing may be inferred from it.
	if s.Status.Card != "" || s.Status.Stage != "" {
		t.Fatalf("summary status unexpectedly names a card: %+v", s.Status)
	}
}

func TestStaleOrUnpairedWorkersAreIgnoredWithReason(t *testing.T) {
	cases := map[string]struct {
		write  func(home string)
		reason string
	}{
		"older than a later single-worker write": {func(home string) {
			writeLanes(t, home, "2026-11-07T10:00:00Z", lane("a", "r1", "BII-1", "preflight", ""), lane("b", "r2", "BII-2", "preflight", ""))
			put(t, home, "status.json", map[string]any{"state": "idle", "updatedAt": "2026-11-07T11:00:00Z"})
		}, "older than status.json"},
		"newer than status.json": {func(home string) {
			writeLanes(t, home, "2026-11-07T11:00:00Z", lane("a", "r1", "BII-1", "preflight", ""), lane("b", "r2", "BII-2", "preflight", ""))
			put(t, home, "status.json", map[string]any{"state": "running", "updatedAt": "2026-11-07T10:00:00Z"})
		}, "newer than status.json"},
		"lanes listed while idle": {func(home string) {
			writeLanes(t, home, "2026-11-07T11:00:00Z", lane("a", "r1", "BII-1", "preflight", ""), lane("b", "r2", "BII-2", "preflight", ""))
			put(t, home, "status.json", map[string]any{"state": "idle", "updatedAt": "2026-11-07T11:00:00Z"})
		}, "not the multi-lane summary"},
		"summary status names a card": {func(home string) {
			writeLanes(t, home, "2026-11-07T11:00:00Z", lane("a", "r1", "BII-1", "preflight", ""), lane("b", "r2", "BII-2", "preflight", ""))
			put(t, home, "status.json", map[string]any{"state": "running", "card": "BII-9", "updatedAt": "2026-11-07T11:00:00Z"})
		}, "not the multi-lane summary"},
		"single lane that status does not mirror": {func(home string) {
			writeLanes(t, home, "2026-11-07T11:00:00Z", lane("a", "r1", "BII-1", "preflight", ""))
			put(t, home, "status.json", map[string]any{"state": "running", "run": "other", "card": "BII-1", "updatedAt": "2026-11-07T11:00:00Z"})
		}, "does not mirror"},
		"no lanes but not idle": {func(home string) {
			writeLanes(t, home, "2026-11-07T11:00:00Z")
			put(t, home, "status.json", map[string]any{"state": "running", "updatedAt": "2026-11-07T11:00:00Z"})
		}, "not idle"},
	}
	for name, c := range cases {
		o, srv := fixture(t)
		c.write(o.home)
		s := getSnapshot(t, srv)
		if s.Workers == nil || s.Workers.Usable || len(s.Workers.Lanes) != 0 || !strings.Contains(s.Workers.Reason, c.reason) {
			t.Errorf("%s: workers = %+v, want unusable with reason containing %q", name, s.Workers, c.reason)
		}
	}
}

func TestSingleMirroredLaneIsUsableAndLabelled(t *testing.T) {
	o, srv := fixture(t)
	log := writeRun(t, o.home, "BII-833", "r-fe", time.Date(2026, 11, 7, 11, 59, 0, 0, time.UTC),
		map[string]any{"kind": "stage", "stage": "preflight", "ts": "2026-11-07T11:59:01Z"})
	writeLanes(t, o.home, "2026-11-07T11:59:50Z", lane("frontend", "r-fe", "BII-833", "preflight", log))
	s := getSnapshot(t, srv)
	if s.Workers == nil || !s.Workers.Usable || len(s.Workers.Lanes) != 1 || s.Workers.Lanes[0].Events[0].Lane != "frontend" {
		t.Fatalf("workers = %+v", s.Workers)
	}
	if s.Status.Card != "BII-833" || !s.LogAvailable {
		t.Fatalf("status.json still mirrors the single lane: %+v", s.Status)
	}
}

func TestMalformedWorkersFileIsAnErrorNotLanes(t *testing.T) {
	bad := map[string]string{
		"not json":          "{nope",
		"wrong version":     `{"version":2,"updatedAt":"2026-11-07T11:00:00Z","workers":[]}`,
		"workers not array": `{"version":1,"updatedAt":"2026-11-07T11:00:00Z","workers":{"a":1}}`,
		"workers null":      `{"version":1,"updatedAt":"2026-11-07T11:00:00Z","workers":null}`,
		"empty lane name":   `{"version":1,"updatedAt":"2026-11-07T11:00:00Z","workers":[{"lane":"","state":"running"}]}`,
		"duplicate lane":    `{"version":1,"updatedAt":"2026-11-07T11:00:00Z","workers":[{"lane":"a"},{"lane":"a"}]}`,
		"too many lanes":    `{"version":1,"updatedAt":"2026-11-07T11:00:00Z","workers":[{"lane":"1"},{"lane":"2"},{"lane":"3"},{"lane":"4"},{"lane":"5"},{"lane":"6"},{"lane":"7"},{"lane":"8"},{"lane":"9"}]}`,
	}
	for name, body := range bad {
		o, srv := fixture(t)
		put(t, o.home, "status.json", map[string]any{"state": "running", "updatedAt": "2026-11-07T11:00:00Z"})
		if err := os.WriteFile(filepath.Join(o.home, "workers.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		s := getSnapshot(t, srv)
		if s.Workers != nil || s.WorkersError == "" {
			t.Errorf("%s: workers=%+v error=%q, want an error and no lanes", name, s.Workers, s.WorkersError)
		}
	}
}

func TestLaneLogOutsideRunsIsRefused(t *testing.T) {
	o, srv := fixture(t)
	outside := filepath.Join(t.TempDir(), "evil.jsonl")
	if err := os.WriteFile(outside, []byte(`{"kind":"start","run":"x","card":"BII-X"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ok := writeRun(t, o.home, "BII-1", "r1", time.Date(2026, 11, 7, 11, 59, 0, 0, time.UTC),
		map[string]any{"kind": "stage", "stage": "preflight"})
	writeLanes(t, o.home, "2026-11-07T11:59:50Z",
		lane("good", "r1", "BII-1", "preflight", ok),
		lane("evil", "r2", "BII-X", "preflight", outside))
	s := getSnapshot(t, srv)
	if s.Workers == nil || len(s.Workers.Lanes) != 2 {
		t.Fatalf("workers = %+v", s.Workers)
	}
	evil := s.Workers.Lanes[1]
	if evil.LogAvailable || evil.LogError == "" || len(evil.Events) != 0 {
		t.Fatalf("a lane log outside the autopilot home was read: %+v", evil)
	}
	if !s.Workers.Lanes[0].LogAvailable {
		t.Fatal("one bad lane must not hide the good lane's log")
	}
}

func TestSSEFollowsEveryLaneLogWithLabels(t *testing.T) {
	o, srv := fixture(t)
	mod := time.Date(2026, 11, 7, 11, 59, 0, 0, time.UTC)
	fe := writeRun(t, o.home, "BII-833", "r-fe", mod, map[string]any{"kind": "stage", "stage": "preflight", "ts": "2026-11-07T11:59:01Z"})
	be := writeRun(t, o.home, "BII-830", "r-be", mod, map[string]any{"kind": "stage", "stage": "implement:1", "ts": "2026-11-07T11:59:02Z"})
	writeLanes(t, o.home, "2026-11-07T11:59:50Z", lane("backend", "r-be", "BII-830", "implement:1", be), lane("frontend", "r-fe", "BII-833", "preflight", fe))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rows := make(chan event, 20)
	go func() {
		defer close(rows)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				var e event
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e) == nil && e.ID != "" {
					rows <- e
				}
			}
		}
	}()
	got := map[string]string{}
	for len(got) < 2 {
		select {
		case row := <-rows:
			got[row.Lane] = str(row.Native, "stage")
		case <-time.After(2 * time.Second):
			t.Fatalf("SSE delivered only %v", got)
		}
	}
	if got["backend"] != "implement:1" || got["frontend"] != "preflight" {
		t.Fatalf("lane labels or records wrong: %v", got)
	}
	// A new record on one lane arrives with that lane's label while the other stays quiet.
	f, _ := os.OpenFile(be, os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprintln(f, `{"kind":"permission","stage":"implement:1","data":{"allow":false,"action":"shell"}}`)
	f.Close()
	select {
	case row := <-rows:
		if row.Lane != "backend" || str(row.Native, "kind") != "permission" || row.History {
			t.Fatalf("live lane record = %+v", row)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no live record from the backend lane")
	}
}

func TestNoFinishedHistoryWhileLanesAreRunning(t *testing.T) {
	o, srv := fixture(t)
	mod := time.Date(2026, 11, 7, 11, 59, 0, 0, time.UTC)
	fe := writeRun(t, o.home, "BII-833", "r-fe", mod, map[string]any{"kind": "stage", "stage": "preflight", "ts": "2026-11-07T11:59:01Z"})
	be := writeRun(t, o.home, "BII-830", "r-be", mod, map[string]any{"kind": "stage", "stage": "implement:1", "ts": "2026-11-07T11:59:02Z"})
	put(t, o.home, "usage.json", map[string]any{"day": "2026-11-07", "today": 90000, "cards": map[string]any{"BII-830": 52000, "BII-833": 19000}})
	writeLanes(t, o.home, "2026-11-07T11:59:50Z", lane("backend", "r-be", "BII-830", "implement:1", be), lane("frontend", "r-fe", "BII-833", "preflight", fe))
	s := getSnapshot(t, srv)
	if s.LastRun != nil || len(s.LastEvents) != 0 {
		t.Fatalf("live lanes were offered as finished history: %+v", s.LastRun)
	}
	b, f := s.Workers.Lanes[0], s.Workers.Lanes[1]
	if b.CardTokens == nil || *b.CardTokens != 52000 || f.CardTokens == nil || *f.CardTokens != 19000 {
		t.Fatalf("per-lane card usage = %v / %v", b.CardTokens, f.CardTokens)
	}
	// Running summary with no detail at all: still not "finished history".
	o2, srv2 := fixture(t)
	writeRun(t, o2.home, "BII-1", "r1", mod, end("BII-1", "r1", "ready-for-review", "2026-11-07T11:00:00Z", nil))
	put(t, o2.home, "status.json", map[string]any{"state": "running", "updatedAt": "2026-11-07T11:59:50Z", "note": "2 lanes active; see workers.json"})
	if s2 := getSnapshot(t, srv2); s2.LastRun != nil {
		t.Fatalf("a running summary was offered finished history: %+v", s2.LastRun)
	}
}
