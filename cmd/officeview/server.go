package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
)

//go:embed assets/*
var assets embed.FS

type office struct {
	home, origin, host, token string
	controls, testData        bool
	now                       func() time.Time
	poll                      time.Duration
	page                      *template.Template
}

func newOffice(home, origin string, controls, testData bool) (*office, error) {
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" {
		return nil, fmt.Errorf("invalid loopback origin")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	page, err := template.ParseFS(assets, "assets/index.html")
	if err != nil {
		return nil, err
	}
	return &office{home: abs, origin: origin, host: u.Host, token: hex.EncodeToString(b), controls: controls, testData: testData, now: time.Now, poll: time.Second, page: page}, nil
}

func (o *office) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	if r.Host != o.host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != o.origin) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	path := r.URL.EscapedPath()
	if path == "/api/stop" || path == "/api/resume" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", 405)
			return
		}
		o.control(w, r, path == "/api/stop")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", 405)
		return
	}
	switch path {
	case "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		o.page.Execute(w, struct{ Token string }{o.token})
	case "/assets/style.css", "/assets/app.js":
		name := strings.TrimPrefix(path, "/")
		b, err := fs.ReadFile(assets, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(name, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		w.Write(b)
	case "/api/state":
		writeJSON(w, o.snapshot())
	case "/api/events":
		o.events(w, r)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (o *office) control(w http.ResponseWriter, r *http.Request, stop bool) {
	if !o.controls {
		http.Error(w, "Controls disabled", http.StatusForbidden)
		return
	}
	token := r.Header.Get("X-Office-Token")
	if r.Header.Get("Origin") != o.origin || subtle.ConstantTimeCompare([]byte(token), []byte(o.token)) != 1 {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	root, err := os.OpenRoot(o.home)
	if err != nil {
		http.Error(w, "Could not change STOP", http.StatusServiceUnavailable)
		return
	}
	defer root.Close()
	// A STOP symlink is never a supported control target, including one
	// pointing within home; do not let SetStop follow it into another file.
	if info, err := root.Lstat("STOP"); err == nil && info.Mode()&os.ModeSymlink != 0 {
		http.Error(w, "Could not change STOP", 409)
		return
	} else if err != nil && !os.IsNotExist(err) {
		http.Error(w, "Could not change STOP", 503)
		return
	}
	paths := runstate.Paths{Root: o.home}
	if stop {
		err = paths.SetStop("manual stop from Graphical Office", o.now())
	} else {
		err = paths.ClearStop()
	}
	if err != nil {
		http.Error(w, "Could not change STOP", 503)
		return
	}
	// The returned snapshot is a fresh readback, never an optimistic value.
	writeJSON(w, o.snapshot())
}

func (o *office) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(o.poll)
	defer ticker.Stop()
	var identity string
	var offset int64
	var historyUntil int64
	var previous os.FileInfo
	lastError := ""
	pump := func() bool {
		root, err := os.OpenRoot(o.home)
		if err != nil {
			return streamError(w, flusher, &lastError, "Activity unavailable")
		}
		defer root.Close()
		st, err := readStatus(root)
		if err != nil {
			return streamError(w, flusher, &lastError, "Status unavailable")
		}
		if st.LogPath == "" {
			if lastError != "" {
				fmt.Fprint(w, "event: telemetry-ready\ndata: {}\n\n")
				flusher.Flush()
				lastError = ""
			}
			return true
		}
		f, id, err := o.openLog(root, st.LogPath)
		if err != nil {
			return streamError(w, flusher, &lastError, "Activity unavailable")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return true
		}
		history := identity != id || previous == nil || !os.SameFile(previous, info) || info.Size() < offset
		if history {
			identity = id
			offset = 0
			historyUntil = info.Size()
		}
		previous = info
		rows, next, err := readEvents(f, identity, offset, 500)
		if err != nil {
			return streamError(w, flusher, &lastError, "Activity unavailable")
		}
		if lastError != "" {
			fmt.Fprint(w, "event: telemetry-ready\ndata: {}\n\n")
		}
		lastError = ""
		offset = next
		for _, row := range rows {
			parts := strings.Split(row.ID, ":")
			start, _ := strconv.ParseInt(parts[1], 10, 64)
			row.History = start < historyUntil
			b, _ := json.Marshal(row)
			if _, err := fmt.Fprintf(w, "id: %s\nevent: log\ndata: %s\n\n", row.ID, b); err != nil {
				return false
			}
		}
		flusher.Flush()
		return true
	}
	if !pump() {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !pump() {
				return
			}
		}
	}
}

func streamError(w http.ResponseWriter, f http.Flusher, last *string, msg string) bool {
	if *last == msg {
		return true
	}
	*last = msg
	b, _ := json.Marshal(map[string]string{"message": msg})
	_, err := fmt.Fprintf(w, "event: telemetry-error\ndata: %s\n\n", b)
	f.Flush()
	return err == nil
}
