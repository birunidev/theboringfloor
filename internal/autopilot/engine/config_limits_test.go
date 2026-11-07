package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
)

func TestTokenCapsRequiredBeforeExecution(t *testing.T) {
	cases := []struct {
		name string
		card int64
		day  int64
		want string
	}{
		{"missing both", 0, 0, "per_card_tokens"},
		{"zero per card", 0, 160000, "per_card_tokens"},
		{"zero daily", 40000, 0, "daily_tokens"},
		{"negative per card", -1, 160000, "per_card_tokens"},
		{"negative daily", 40000, -1, "daily_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			body := fmt.Sprintf(`{"per_card_tokens":%d,"daily_tokens":%d,"boards":[{"board_id":"b","repo_name":"r","repo_dir":"/tmp","label":"none","ready_list":"Autopilot Queue"}]}`, tc.card, tc.day)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadConfig error = %v; expected rejection of %s", err, tc.want)
			}
		})
	}
}

func TestTokenCapsExactPositiveConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"per_card_tokens":40000,"daily_tokens":160000,"boards":[{"board_id":"b","repo_name":"r","repo_dir":"/tmp","label":"none","ready_list":"Autopilot Queue"}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.PerCardTokens != 40000 || c.DailyTokens != 160000 {
		t.Fatalf("limits changed: per_card=%d daily=%d", c.PerCardTokens, c.DailyTokens)
	}
}

func TestTokenCapsEnforceCardAndDailyAcrossRestart(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC) }
	path := filepath.Join(t.TempDir(), "usage.json")
	b, err := runstate.OpenBudget(path, 40000, 160000, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Charge("FE", 39999); err != nil {
		t.Fatal(err)
	}
	if err := b.Charge("FE", 1); !errors.Is(err, runstate.ErrCardCap) {
		t.Fatalf("expected per-card cap at 40000, got %v", err)
	}
	if err := b.Allow("FE"); !errors.Is(err, runstate.ErrCardCap) {
		t.Fatalf("expected continued card enforcement, got %v", err)
	}
	for _, card := range []string{"BE", "DOC1"} {
		if err := b.Charge(card, 39999); err != nil {
			t.Fatal(err)
		}
		if err := b.Charge(card, 1); !errors.Is(err, runstate.ErrCardCap) {
			t.Fatalf("%s: expected per-card cap, got %v", card, err)
		}
	}
	if err := b.Charge("DOC2", 39999); err != nil {
		t.Fatal(err)
	}
	if err := b.Charge("DOC2", 1); !errors.Is(err, runstate.ErrDailyCap) {
		t.Fatalf("expected daily cap at 160000, got %v", err)
	}
	next, err := runstate.OpenBudget(path, 40000, 160000, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Allow("NEW"); !errors.Is(err, runstate.ErrDailyCap) {
		t.Fatalf("daily limit must persist across restart, got %v", err)
	}
	if got := next.Snapshot().Today; got != 160000 {
		t.Fatalf("persisted usage = %d, want 160000", got)
	}
}
