package runstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	// ErrCardCap: the card has spent its token cap.
	ErrCardCap = errors.New("autopilot: per-card token cap reached")
	// ErrDailyCap: today's token cap is spent.
	ErrDailyCap = errors.New("autopilot: daily token cap reached")
)

// Usage is the persisted token ledger. Day is the local calendar day the
// daily counter belongs to; card totals are lifetime (a card retried
// tomorrow does not get a fresh cap).
type Usage struct {
	Day   string           `json:"day"`
	Today int64            `json:"today"`
	Cards map[string]int64 `json:"cards"`
}

// Budget enforces per-card and daily token caps, persisted to usage.json so
// a restart cannot reset them. A cap <= 0 disables that cap.
type Budget struct {
	mu      sync.Mutex
	path    string
	perCard int64
	daily   int64
	now     func() time.Time
	u       Usage
}

// OpenBudget loads (or starts) the ledger at path.
func OpenBudget(path string, perCard, daily int64, now func() time.Time) (*Budget, error) {
	if now == nil {
		now = time.Now
	}
	b := &Budget{path: path, perCard: perCard, daily: daily, now: now, u: Usage{Cards: map[string]int64{}}}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, &b.u); err != nil {
			return nil, fmt.Errorf("usage ledger %s is corrupt: %w", path, err)
		}
		if b.u.Cards == nil {
			b.u.Cards = map[string]int64{}
		}
	}
	return b, nil
}

func (b *Budget) rollLocked() {
	day := b.now().Format("2006-01-02")
	if b.u.Day != day {
		b.u.Day = day
		b.u.Today = 0
	}
}

func (b *Budget) checkLocked(card string) error {
	if b.daily > 0 && b.u.Today >= b.daily {
		return fmt.Errorf("%w (%d/%d today)", ErrDailyCap, b.u.Today, b.daily)
	}
	if b.perCard > 0 && b.u.Cards[card] >= b.perCard {
		return fmt.Errorf("%w (%s: %d/%d)", ErrCardCap, card, b.u.Cards[card], b.perCard)
	}
	return nil
}

// Allow reports whether card may start or continue spending tokens.
func (b *Budget) Allow(card string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	return b.checkLocked(card)
}

// Charge records n tokens for card, persists the ledger, and returns a cap
// error when the charge reached or crossed a cap (the caller then aborts
// the session). Tokens are always recorded, even when over the cap.
func (b *Budget) Charge(card string, n int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	if n > 0 {
		b.u.Today += n
		b.u.Cards[card] += n
	}
	if err := writeJSONAtomic(b.path, b.u); err != nil {
		return fmt.Errorf("persist usage ledger: %w", err)
	}
	return b.checkLocked(card)
}

// Snapshot returns a copy of the ledger.
func (b *Budget) Snapshot() Usage {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	c := Usage{Day: b.u.Day, Today: b.u.Today, Cards: map[string]int64{}}
	for k, v := range b.u.Cards {
		c.Cards[k] = v
	}
	return c
}
