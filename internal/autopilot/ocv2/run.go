package ocv2

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Decision is an Approver's answer to one permission request.
type Decision struct {
	Allow  bool
	Reason string
}

// Approver decides permission requests in code. It must fail closed:
// anything it does not recognize is rejected.
type Approver interface {
	Decide(PermissionRequest) Decision
}

// ApproverFunc adapts a function to Approver.
type ApproverFunc func(PermissionRequest) Decision

func (f ApproverFunc) Decide(r PermissionRequest) Decision { return f(r) }

// TurnEvent is reported to TurnSpec.OnEvent for the run log.
type TurnEvent struct {
	Kind     string // "permission", "tokens", "interrupt"
	Request  *PermissionRequest
	Decision *Decision
	Tokens   *Tokens
	Note     string
}

// TurnSpec is one prompt-and-wait on an existing session.
type TurnSpec struct {
	SessionID string
	Text      string
	Approver  Approver
	// OnTokens sees cumulative session usage after every poll; an error
	// interrupts the turn (token cap).
	OnTokens func(Tokens) error
	OnEvent  func(TurnEvent)
	Poll     time.Duration // default 750ms
}

// TurnResult is what a finished turn produced.
type TurnResult struct {
	Text    string // final assistant text of this turn
	Outcome string // idle outcome, e.g. "succeeded"
	Tokens  Tokens // cumulative session usage at the end
}

var (
	// ErrTurnFailed: the turn ended with an outcome other than succeeded.
	ErrTurnFailed = errors.New("ocv2: turn did not succeed")
	// ErrInterrupted: the turn was interrupted by cap, timeout or cancel.
	ErrInterrupted = errors.New("ocv2: turn interrupted")
)

// RunTurn prompts the session and drives it to its next idle, answering
// every permission request through spec.Approver.
func (c *Client) RunTurn(ctx context.Context, spec TurnSpec) (TurnResult, error) {
	if spec.Approver == nil {
		return TurnResult{}, errors.New("ocv2: RunTurn needs an Approver")
	}
	poll := spec.Poll
	if poll <= 0 {
		poll = 750 * time.Millisecond
	}
	emit := func(e TurnEvent) {
		if spec.OnEvent != nil {
			spec.OnEvent(e)
		}
	}
	before, err := c.Messages(ctx, spec.SessionID)
	if err != nil {
		return TurnResult{}, err
	}
	seen := map[string]bool{}
	for _, m := range before {
		seen[m.ID] = true
	}
	if err := c.Prompt(ctx, spec.SessionID, spec.Text); err != nil {
		return TurnResult{}, err
	}

	interrupt := func(cause error) (TurnResult, error) {
		ictx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ierr := c.Interrupt(ictx, spec.SessionID)
		emit(TurnEvent{Kind: "interrupt", Note: cause.Error()})
		if ierr != nil {
			return TurnResult{}, fmt.Errorf("%w: %v (interrupt also failed: %v)", ErrInterrupted, cause, ierr)
		}
		return TurnResult{}, fmt.Errorf("%w: %w", ErrInterrupted, cause)
	}

	t := time.NewTicker(poll)
	defer t.Stop()
	var last Tokens
	for {
		select {
		case <-ctx.Done():
			return interrupt(ctx.Err())
		case <-t.C:
		}

		perms, err := c.Permissions(ctx, spec.SessionID)
		if err != nil && ctx.Err() == nil {
			return interrupt(fmt.Errorf("list permissions: %w", err))
		}
		for i := range perms {
			r := perms[i]
			d := spec.Approver.Decide(r)
			decision := "reject"
			if d.Allow {
				decision = "once"
			}
			emit(TurnEvent{Kind: "permission", Request: &r, Decision: &d})
			if err := c.Reply(ctx, spec.SessionID, r.ID, decision, d.Reason); err != nil && ctx.Err() == nil {
				return interrupt(fmt.Errorf("reply permission %s: %w", r.ID, err))
			}
		}

		tok, err := c.SessionTokens(ctx, spec.SessionID)
		if err == nil {
			if tok != last {
				last = tok
				emit(TurnEvent{Kind: "tokens", Tokens: &tok})
			}
			if spec.OnTokens != nil {
				if cerr := spec.OnTokens(tok); cerr != nil {
					return interrupt(cerr)
				}
			}
		}

		msgs, err := c.Messages(ctx, spec.SessionID)
		if err != nil {
			continue
		}
		// Newest first: the first unseen idle closes this turn; the first
		// unseen assistant message carries its answer.
		var idle *Message
		text := ""
		gotText := false
		for i := range msgs {
			m := msgs[i]
			if seen[m.ID] {
				continue
			}
			if m.Type == "idle" && idle == nil {
				idle = &m
			}
			if m.Type == "assistant" && !gotText {
				text, gotText = m.Text(), true
			}
		}
		if idle == nil {
			continue
		}
		res := TurnResult{Text: text, Outcome: idle.Outcome, Tokens: last}
		if idle.Outcome != "succeeded" {
			return res, fmt.Errorf("%w: outcome %q", ErrTurnFailed, idle.Outcome)
		}
		return res, nil
	}
}
