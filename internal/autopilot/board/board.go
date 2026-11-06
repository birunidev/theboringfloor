// Package board is the autopilot's view of a task board: list the cards in
// a list, read one card, move it, and comment on it. The autopilot never
// talks to a board implementation directly; it goes through Guard, which
// refuses every move into a forbidden list (Done). That rule lives here, in
// code, so no prompt or model output can route around it.
package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Card is the subset of a board card the autopilot reads.
type Card struct {
	ID          string
	Key         string // human key, e.g. "BII-655"
	BoardID     string
	ListID      string
	Title       string
	Description string
	LabelIDs    []string
	Comments    []Comment
	UpdatedAt   string
}

// Comment is one card comment.
type Comment struct {
	ID   string
	Text string
	At   string
}

// Ref returns the human key when present, else the stable id.
func (c Card) Ref() string {
	if c.Key != "" {
		return c.Key
	}
	return c.ID
}

// HasLabel reports whether the card carries the label id.
func (c Card) HasLabel(id string) bool {
	for _, l := range c.LabelIDs {
		if l == id {
			return true
		}
	}
	return false
}

// Board is a task board the autopilot can read and write.
type Board interface {
	ListCards(ctx context.Context, boardID, listID string) ([]Card, error)
	GetCard(ctx context.Context, cardID string) (Card, error)
	// MoveCard moves a card. idemKey makes a retried call a no-op.
	MoveCard(ctx context.Context, cardID, listID, idemKey string) error
	// AddComment comments on a card. idemKey makes a retried call a no-op.
	AddComment(ctx context.Context, cardID, text, idemKey string) error
}

// ErrForbiddenMove is returned by Guard for a move into a forbidden list.
var ErrForbiddenMove = errors.New("board: move into a forbidden list refused")

// Guard wraps a Board and refuses moves into forbidden lists.
type Guard struct {
	inner     Board
	forbidden map[string]bool
}

// NewGuard wraps inner. forbiddenListIDs are list ids no card may enter
// (the Done list of every board the autopilot works). Empty ids are ignored.
func NewGuard(inner Board, forbiddenListIDs ...string) *Guard {
	g := &Guard{inner: inner, forbidden: map[string]bool{}}
	for _, id := range forbiddenListIDs {
		if id = strings.TrimSpace(id); id != "" {
			g.forbidden[id] = true
		}
	}
	return g
}

func (g *Guard) ListCards(ctx context.Context, boardID, listID string) ([]Card, error) {
	return g.inner.ListCards(ctx, boardID, listID)
}

func (g *Guard) GetCard(ctx context.Context, cardID string) (Card, error) {
	return g.inner.GetCard(ctx, cardID)
}

func (g *Guard) MoveCard(ctx context.Context, cardID, listID, idemKey string) error {
	if g.forbidden[strings.TrimSpace(listID)] {
		return fmt.Errorf("%w: card %s -> list %s", ErrForbiddenMove, cardID, listID)
	}
	return g.inner.MoveCard(ctx, cardID, listID, idemKey)
}

func (g *Guard) AddComment(ctx context.Context, cardID, text, idemKey string) error {
	return g.inner.AddComment(ctx, cardID, text, idemKey)
}
