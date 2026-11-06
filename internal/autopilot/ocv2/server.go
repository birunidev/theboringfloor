package ocv2

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Server is one private `opencode serve` owned by the autopilot.
type Server struct {
	URL      string
	Password string
	cmd      *exec.Cmd
	done     chan struct{}
	waitErr  error
	once     sync.Once
}

// Bin is the opencode executable; THEFLOOR_OPENCODE_BIN overrides it.
func Bin() string {
	if v := os.Getenv("THEFLOOR_OPENCODE_BIN"); v != "" {
		return v
	}
	return "opencode"
}

var listenRe = regexp.MustCompile(`listening on (https?://[^\s]+)`)

// stripEnv are inherited variables a serve must never see: a process-wide
// permission override would bypass the per-session ruleset.
var stripEnv = []string{"OPENCODE_CONFIG_CONTENT", "OPENCODE_SERVER_PASSWORD", "OPENCODE_PERMISSION"}

// Spawn starts `opencode serve` on a random loopback port in dir with a
// fresh random password, and waits up to 20s for its URL.
func Spawn(ctx context.Context, dir string) (*Server, error) {
	pw, err := randomPassword()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(Bin(), "serve", "--port", "0", "--hostname", "127.0.0.1")
	cmd.Dir = dir
	env := os.Environ()
	for _, k := range stripEnv {
		env = withoutEnv(env, k)
	}
	cmd.Env = append(env, "OPENCODE_SERVER_PASSWORD="+pw)
	setProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("opencode serve: %w", err)
	}
	s := &Server{Password: pw, cmd: cmd, done: make(chan struct{})}
	urlCh := make(chan string, 1)
	var tail strings.Builder
	var tailMu sync.Mutex
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		sent := false
		for sc.Scan() {
			line := sc.Text()
			if !sent {
				tailMu.Lock()
				if tail.Len() < 4000 {
					tail.WriteString(line + "\n")
				}
				tailMu.Unlock()
				if m := listenRe.FindStringSubmatch(line); m != nil {
					urlCh <- strings.TrimRight(m[1], "/.")
					sent = true
				}
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() {
		s.waitErr = cmd.Wait()
		close(s.done)
	}()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	select {
	case u := <-urlCh:
		s.URL = u
		return s, nil
	case <-s.done:
		tailMu.Lock()
		defer tailMu.Unlock()
		return nil, fmt.Errorf("opencode serve exited before listening: %v: %s", s.waitErr, trim(tail.String(), 400))
	case <-timer.C:
		s.Close()
		return nil, errors.New("opencode serve: no listening URL within 20s")
	case <-ctx.Done():
		s.Close()
		return nil, ctx.Err()
	}
}

// Client returns an API client for this server.
func (s *Server) Client() *Client { return NewClient(s.URL, s.Password) }

// Close stops the server's whole process group: TERM, then KILL after 5s.
func (s *Server) Close() error {
	s.once.Do(func() {
		_ = signalGroup(s.cmd, false)
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			_ = signalGroup(s.cmd, true)
			<-s.done
		}
	})
	return nil
}

func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func withoutEnv(env []string, key string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}
