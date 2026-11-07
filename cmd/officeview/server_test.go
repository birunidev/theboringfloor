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
