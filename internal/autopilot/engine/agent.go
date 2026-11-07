package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/ocv2"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
)

// OCAgent runs pipeline stages as opencode v2 sessions in one worktree.
// Every session is ask-everything; Policy decides each request in code.
type OCAgent struct {
	Client      *ocv2.Client
	Dir         string
	Model       ocv2.ModelRef
	Cfg         Config
	Checks      []Check
	DoneListIDs []string
	Card        string
	Budget      *runstate.Budget
	Log         *runstate.RunLog
	OnStage     func(name string)

	mu    sync.Mutex
	spent map[string]int64 // session -> billable already charged
}

func (a *OCAgent) policy(role pipeline.Role) *ocv2.Policy {
	p := &ocv2.Policy{Root: a.Dir, ReadActions: a.Cfg.ReadActions, PatternActions: a.Cfg.PatternActions, BashAction: a.Cfg.BashAction}
	switch role {
	case pipeline.RolePreflight:
		p.Space = ocv2.NewSpaceGate(ocv2.DefaultSpaceReads, a.DoneListIDs...)
	case pipeline.RoleImplement:
		p.WriteActions = a.Cfg.WriteActions
		// Check commands run agent-written code, so only the autopilot runs
		// them, inside the bwrap sandbox. The implementer gets read-only git.
		p.Bash = append(p.Bash, a.Cfg.ExtraBash...)
		p.Bash = append(p.Bash, "git status", "git diff", "git log", "ls")
	}
	return p
}

// Run implements pipeline.Agent.
func (a *OCAgent) Run(ctx context.Context, req pipeline.StageRequest) (pipeline.StageResponse, error) {
	if a.OnStage != nil {
		a.OnStage(req.Name)
	}
	if err := a.Budget.Allow(a.Card); err != nil {
		return pipeline.StageResponse{}, err
	}
	sid := req.Session
	if sid == "" {
		var err error
		sid, err = a.Client.CreateSession(ctx, ocv2.SessionSpec{
			Title: fmt.Sprintf("autopilot %s %s", a.Card, req.Name), Dir: a.Dir, Model: a.Model, Rules: ocv2.AskEverything,
		})
		if err != nil {
			return pipeline.StageResponse{}, err
		}
	}
	a.logEvent(runstate.Event{Kind: "stage", Stage: req.Name, Msg: "session " + sid})
	res, err := a.Client.RunTurn(ctx, ocv2.TurnSpec{
		SessionID: sid,
		Text:      req.Prompt,
		Approver:  a.policy(req.Role),
		OnTokens:  func(t ocv2.Tokens) error { return a.charge(sid, t.Billable()) },
		OnEvent: func(e ocv2.TurnEvent) {
			if e.Kind == "permission" {
				a.logEvent(runstate.Event{Kind: "permission", Stage: req.Name, Msg: e.Decision.Reason,
					Data: map[string]any{"action": e.Request.Action, "resources": e.Request.Resources, "allow": e.Decision.Allow}})
			}
			if e.Kind == "note" {
				a.logEvent(runstate.Event{Kind: "note", Stage: req.Name, Msg: e.Note})
			}
			if e.Kind == "interrupt" {
				a.logEvent(runstate.Event{Kind: "error", Stage: req.Name, Msg: "interrupted: " + e.Note})
			}
		},
	})
	if err != nil {
		return pipeline.StageResponse{Session: sid}, err
	}
	a.logEvent(runstate.Event{Kind: "stage-done", Stage: req.Name, Msg: clip(res.Text, 2000), Data: map[string]any{"billable": res.Tokens.Billable()}})
	return pipeline.StageResponse{Text: res.Text, Session: sid}, nil
}

// charge bills the delta since the session's last report.
func (a *OCAgent) charge(sid string, total int64) error {
	a.mu.Lock()
	if a.spent == nil {
		a.spent = map[string]int64{}
	}
	delta := total - a.spent[sid]
	if delta > 0 {
		a.spent[sid] = total
	}
	a.mu.Unlock()
	if delta <= 0 {
		return nil
	}
	return a.Budget.Charge(a.Card, delta)
}

func (a *OCAgent) logEvent(e runstate.Event) {
	if a.Log != nil {
		_ = a.Log.Write(e)
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
