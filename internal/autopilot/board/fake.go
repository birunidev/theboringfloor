package board

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Move records one MoveCard call on a Fake.
type Move struct {
	CardID, ListID, IdemKey string
}

// Fake is an in-memory Board for tests. Idempotency keys are honored the
// way the real board honors them: a repeated key is a no-op.
type Fake struct {
	mu       sync.Mutex
	cards    map[string]*Card // by stable id
	seen     map[string]bool  // idempotency keys already applied
	Moves    []Move
	Comments map[string][]string // card id -> comment texts, in order
	// Err, when set, is returned by every call (transport failure).
	Err error
}

// NewFake seeds a Fake with cards.
func NewFake(cards ...Card) *Fake {
	f := &Fake{cards: map[string]*Card{}, seen: map[string]bool{}, Comments: map[string][]string{}}
	for i := range cards {
		c := cards[i]
		f.cards[c.ID] = &c
	}
	return f
}

func (f *Fake) resolve(ref string) (*Card, error) {
	if c, ok := f.cards[ref]; ok {
		return c, nil
	}
	for _, c := range f.cards {
		if c.Key == ref {
			return c, nil
		}
	}
	return nil, fmt.Errorf("card %q not found", ref)
}

func (f *Fake) ListCards(_ context.Context, boardID, listID string) ([]Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var out []Card
	for _, c := range f.cards {
		if c.BoardID == boardID && (listID == "" || c.ListID == listID) {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *Fake) GetCard(_ context.Context, cardID string) (Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return Card{}, f.Err
	}
	c, err := f.resolve(cardID)
	if err != nil {
		return Card{}, err
	}
	return *c, nil
}

func (f *Fake) MoveCard(_ context.Context, cardID, listID, idemKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	c, err := f.resolve(cardID)
	if err != nil {
		return err
	}
	if idemKey != "" {
		if f.seen["move:"+idemKey] {
			return nil
		}
		f.seen["move:"+idemKey] = true
	}
	c.ListID = listID
	f.Moves = append(f.Moves, Move{CardID: c.ID, ListID: listID, IdemKey: idemKey})
	return nil
}

func (f *Fake) AddComment(_ context.Context, cardID, text, idemKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	c, err := f.resolve(cardID)
	if err != nil {
		return err
	}
	if idemKey != "" {
		if f.seen["comment:"+idemKey] {
			return nil
		}
		f.seen["comment:"+idemKey] = true
	}
	c.Comments = append(c.Comments, Comment{ID: fmt.Sprintf("c%d", len(c.Comments)+1), Text: text})
	f.Comments[c.ID] = append(f.Comments[c.ID], text)
	return nil
}
