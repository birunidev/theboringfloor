package spaceboard

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveRead reads (never writes) the real Space board. Skipped unless
// AUTOPILOT_LIVE=1.
func TestLiveRead(t *testing.T) {
	if os.Getenv("AUTOPILOT_LIVE") != "1" {
		t.Skip("set AUTOPILOT_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := New(NewTokenSource(DefaultAuthFile(), ""))
	b, err := c.GetBoard(ctx, "board_938353e08e72")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("board %q: %d lists, %d labels; Todo=%s Done=%s Review=%s OnHold=%s", b.Name, len(b.Lists), len(b.Labels),
		b.ListID("Todo"), b.ListID("Done"), b.ListID("Review"), b.ListID("On Hold"))
	card, err := c.GetCard(ctx, "BII-655")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("card %s %q list=%s comments=%d", card.Key, card.Title, card.ListID, len(card.Comments))
	todo, err := c.ListCards(ctx, b.ID, b.ListID("Todo"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Todo has %d cards", len(todo))
}
