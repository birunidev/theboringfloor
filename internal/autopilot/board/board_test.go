package board

import (
	"context"
	"errors"
	"testing"
)

func seed() *Fake {
	return NewFake(
		Card{ID: "c1", Key: "BII-1", BoardID: "b", ListID: "todo", Title: "one", LabelIDs: []string{"auto"}},
		Card{ID: "c2", Key: "BII-2", BoardID: "b", ListID: "todo", Title: "two"},
		Card{ID: "c3", Key: "BII-3", BoardID: "other", ListID: "todo", Title: "three"},
	)
}

func TestGuardRefusesDone(t *testing.T) {
	f := seed()
	g := NewGuard(f, "done", " ", "")
	err := g.MoveCard(context.Background(), "BII-1", "done", "k1")
	if !errors.Is(err, ErrForbiddenMove) {
		t.Fatalf("want ErrForbiddenMove, got %v", err)
	}
	if len(f.Moves) != 0 {
		t.Fatalf("forbidden move reached the board: %+v", f.Moves)
	}
	// Whitespace around the id must not slip past the guard.
	if err := g.MoveCard(context.Background(), "BII-1", " done ", "k2"); !errors.Is(err, ErrForbiddenMove) {
		t.Fatalf("padded id slipped past guard: %v", err)
	}
}

func TestGuardAllowsOtherLists(t *testing.T) {
	f := seed()
	g := NewGuard(f, "done")
	if err := g.MoveCard(context.Background(), "BII-1", "review", "k1"); err != nil {
		t.Fatal(err)
	}
	c, _ := g.GetCard(context.Background(), "c1")
	if c.ListID != "review" {
		t.Fatalf("list = %q", c.ListID)
	}
}

func TestFakeIdempotency(t *testing.T) {
	f := seed()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := f.AddComment(ctx, "BII-2", "hello", "same"); err != nil {
			t.Fatal(err)
		}
		if err := f.MoveCard(ctx, "BII-2", "wip", "same"); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(f.Comments["c2"]); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}
	if n := len(f.Moves); n != 1 {
		t.Fatalf("moves = %d, want 1", n)
	}
}

func TestFakeListFiltersBoardAndList(t *testing.T) {
	f := seed()
	cards, err := f.ListCards(context.Background(), "b", "todo")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Ref() != "BII-1" || !cards[0].HasLabel("auto") || cards[1].HasLabel("auto") {
		t.Fatalf("cards = %+v", cards)
	}
}

func TestFakeErr(t *testing.T) {
	f := seed()
	f.Err = errors.New("offline")
	if _, err := f.ListCards(context.Background(), "b", ""); err == nil {
		t.Fatal("want error")
	}
}
