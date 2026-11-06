package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/handoff"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/worktree"
)

// SpaceAPI is the board plus the board metadata the engine needs.
type SpaceAPI interface {
	board.Board
	GetBoard(ctx context.Context, boardID string) (spaceboard.BoardInfo, error)
}

// AgentFactory starts an agent rooted at dir (e.g. an opencode server) and
// returns it with a cleanup function.
type AgentFactory func(ctx context.Context, dir string, card board.Card, bc BoardConfig, doneListIDs []string, log *runstate.RunLog, onStage func(string)) (pipeline.Agent, func(), error)

// Publisher commits, pushes and opens the draft PR (see package publish).
type Publisher interface {
	Publish(ctx context.Context, dir, branch, base, commitMsg, prTitle, prBody string) (prURL string, err error)
}

// Engine runs cards.
type Engine struct {
	Cfg      Config
	Paths    runstate.Paths
	Space    SpaceAPI
	Budget   *runstate.Budget
	WT       worktree.Manager
	NewAgent AgentFactory
	Pub      Publisher
	Now      func() time.Time
	Out      io.Writer
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) printf(format string, a ...any) {
	if e.Out != nil {
		fmt.Fprintf(e.Out, format, a...)
	}
}

// Resolved is a board with its list and label ids looked up.
type Resolved struct {
	BC                                             BoardConfig
	Info                                           spaceboard.BoardInfo
	Ready, Working, Review, Blocked, Clarify, Done string
	Label                                          string
	Policy                                         string
	ClarifyFallback                                bool
}

// Resolve looks up list and label ids and assembles the board policy text.
func (e *Engine) Resolve(ctx context.Context, bc BoardConfig) (Resolved, error) {
	info, err := e.Space.GetBoard(ctx, bc.BoardID)
	if err != nil {
		return Resolved{}, err
	}
	r := Resolved{BC: bc, Info: info,
		Ready: info.ListID(bc.ReadyList), Review: info.ListID(bc.ReviewList), Blocked: info.ListID(bc.BlockedList),
		Clarify: info.ListID(bc.ClarifyList), Done: info.ListID(bc.DoneList), Label: info.LabelID(bc.Label)}
	if NoLabel(bc.Label) {
		// A dedicated queue list is the switch: every card in it is eligible.
		r.Label = anyLabel
	}
	if bc.WorkingList != "" {
		r.Working = info.ListID(bc.WorkingList)
		if r.Working == "" {
			return r, fmt.Errorf("board %s: working list %q not found", info.Name, bc.WorkingList)
		}
	}
	switch {
	case r.Ready == "":
		return r, fmt.Errorf("board %s: ready list %q not found", info.Name, bc.ReadyList)
	case r.Review == "":
		return r, fmt.Errorf("board %s: review list %q not found", info.Name, bc.ReviewList)
	case r.Blocked == "":
		return r, fmt.Errorf("board %s: blocked list %q not found", info.Name, bc.BlockedList)
	case r.Done == "":
		return r, fmt.Errorf("board %s: done list %q not found (needed to forbid it)", info.Name, bc.DoneList)
	}
	if r.Clarify == "" {
		r.Clarify, r.ClarifyFallback = r.Blocked, true
	}
	var pol strings.Builder
	pol.WriteString(fmt.Sprintf("Board: %s\n\n%s\n", info.Name, info.Description))
	for _, ref := range bc.PolicyCards {
		c, err := e.Space.GetCard(ctx, ref)
		if err != nil {
			return r, fmt.Errorf("policy card %s: %w", ref, err)
		}
		pol.WriteString(fmt.Sprintf("\n--- %s %s ---\n%s\n", c.Key, c.Title, clip(c.Description, 12000)))
	}
	r.Policy = pol.String()
	return r, nil
}

const anyLabel = "\x00any"

// NoLabel reports a config that selects cards by list alone ("label": "none").
func NoLabel(label string) bool { return strings.EqualFold(strings.TrimSpace(label), "none") }

// Candidates are the ready-list cards carrying the autopilot label (or all
// of them when the board uses a dedicated queue list).
func (e *Engine) Candidates(ctx context.Context, r Resolved) ([]board.Card, error) {
	if r.Label == "" {
		return nil, nil
	}
	cards, err := e.Space.ListCards(ctx, r.BC.BoardID, r.Ready)
	if err != nil {
		return nil, err
	}
	var out []board.Card
	for _, c := range cards {
		if (r.Label == anyLabel || c.HasLabel(r.Label)) && c.ListID == r.Ready {
			out = append(out, c)
		}
	}
	return out, nil
}

func cardContext(c board.Card, r Resolved) pipeline.CardContext {
	cc := pipeline.CardContext{Key: c.Ref(), Title: c.Title, Description: c.Description, BoardPolicy: r.Policy, Repo: r.BC.RepoName}
	for _, m := range c.Comments {
		cc.Comments = append(cc.Comments, m.Text)
	}
	for _, ch := range r.BC.Checks {
		cc.Checks = append(cc.Checks, strings.Join(ch.Argv, " "))
	}
	return cc
}

func (e *Engine) repo(bc BoardConfig) worktree.Repo {
	return worktree.Repo{Name: bc.RepoName, Dir: bc.RepoDir, Base: bc.Base}
}

// readOnly makes every board write fail: the dry run's belt and braces.
type readOnly struct{ SpaceAPI }

var errDryRunWrite = errors.New("dry run: board writes are disabled")

func (readOnly) MoveCard(context.Context, string, string, string) error   { return errDryRunWrite }
func (readOnly) AddComment(context.Context, string, string, string) error { return errDryRunWrite }

// DryRunResult is what a dry run found for one card.
type DryRunResult struct {
	Card      board.Card
	Preflight pipeline.Preflight
	Err       error
	Would     string // the comment the live run would post
	WouldList string // the list the live run would move the card to
}

// DryRun runs pre-flight only, in a detached throwaway worktree, with all
// board writes disabled. cardRef selects one card (any list, label not
// required); empty means the labelled candidates of the ready list.
func (e *Engine) DryRun(ctx context.Context, bc BoardConfig, cardRef string) ([]DryRunResult, error) {
	saved := e.Space
	e.Space = readOnly{saved}
	defer func() { e.Space = saved }()

	r, err := e.Resolve(ctx, bc)
	if err != nil {
		return nil, err
	}
	var cards []board.Card
	if cardRef != "" {
		c, err := e.Space.GetCard(ctx, cardRef)
		if err != nil {
			return nil, err
		}
		cards = []board.Card{c}
	} else {
		if r.Label == "" {
			e.printf("board %s has no %q label yet: nothing is eligible (use --card to dry-run one card)\n", r.Info.Name, bc.Label)
		}
		if cards, err = e.Candidates(ctx, r); err != nil {
			return nil, err
		}
	}
	var out []DryRunResult
	for _, sum := range cards {
		c := sum
		if full, err := e.Space.GetCard(ctx, sum.ID); err == nil {
			c = full
		}
		out = append(out, e.dryRunCard(ctx, r, c))
	}
	return out, nil
}

func (e *Engine) dryRunCard(ctx context.Context, r Resolved, c board.Card) DryRunResult {
	res := DryRunResult{Card: c}
	runID := runstate.NewRunID(e.now())
	log, err := e.Paths.OpenRunLog(c.Ref(), runID+"-dry", e.Now)
	if err != nil {
		res.Err = err
		return res
	}
	defer log.Close()
	_ = log.Write(runstate.Event{Kind: "start", Msg: "dry run (pre-flight only, no board writes)"})
	wt, err := e.WT.PrepareDetached(ctx, e.repo(r.BC), c.Ref())
	if err != nil {
		res.Err = err
		return res
	}
	defer e.WT.Remove(context.Background(), e.repo(r.BC), wt, true)
	agent, cleanup, err := e.NewAgent(ctx, wt.Path, c, r.BC, []string{r.Done}, log, nil)
	if err != nil {
		res.Err = err
		return res
	}
	defer cleanup()
	pre, err := agent.Run(ctx, pipeline.StageRequest{Role: pipeline.RolePreflight, Name: "preflight", Prompt: pipeline.PreflightPrompt(cardContext(c, r))})
	if err != nil {
		res.Err = err
		return res
	}
	p, err := pipeline.ParsePreflight(pre.Text)
	if err != nil {
		res.Err = fmt.Errorf("invalid pre-flight output: %w", err)
		_ = log.Write(runstate.Event{Kind: "error", Msg: res.Err.Error(), Data: map[string]any{"raw": clip(pre.Text, 4000)}})
		return res
	}
	res.Preflight = p
	run := handoff.Run{ID: runID, CardKey: c.Ref(), Title: c.Title, Repo: r.BC.RepoName, When: e.now()}
	if p.Ready {
		res.WouldList = r.BC.WorkingList
		res.Would = fmt.Sprintf("(ready) would implement within %v against %d acceptance criteria", p.ScopePaths, len(p.AcceptanceCriteria))
	} else {
		res.WouldList = r.BC.ClarifyList
		if r.ClarifyFallback {
			res.WouldList = r.BC.BlockedList + " (no " + r.BC.ClarifyList + " list on the board)"
		}
		res.Would = handoff.ClarificationComment(run, p, "")
	}
	_ = log.Write(runstate.Event{Kind: "end", Msg: "dry run done", Data: map[string]any{"ready": p.Ready, "policy_block": p.PolicyBlock}})
	return res
}

// ErrNothingToDo: no eligible card on any board.
var ErrNothingToDo = errors.New("no eligible card")

// Tick works at most one card across all boards (live).
func (e *Engine) Tick(ctx context.Context) error {
	if err := e.Paths.CheckStop(); err != nil {
		return err
	}
	for _, bc := range e.Cfg.Boards {
		r, err := e.Resolve(ctx, bc)
		if err != nil {
			e.printf("board %s: %v\n", bc.Name, err)
			continue
		}
		cands, err := e.Candidates(ctx, r)
		if err != nil {
			e.printf("board %s: %v\n", bc.Name, err)
			continue
		}
		for _, c := range cands {
			if err := e.Budget.Allow(c.Ref()); err != nil {
				e.printf("skip %s: %v\n", c.Ref(), err)
				continue
			}
			return e.RunCard(ctx, r, c)
		}
	}
	return ErrNothingToDo
}

// RunCard takes one card through claim, pipeline and outcome (live).
func (e *Engine) RunCard(ctx context.Context, r Resolved, summary board.Card) error {
	g := board.NewGuard(e.Space, r.Done)
	c, err := e.Space.GetCard(ctx, summary.ID)
	if err != nil {
		return err
	}
	runID := runstate.NewRunID(e.now())
	log, err := e.Paths.OpenRunLog(c.Ref(), runID, e.Now)
	if err != nil {
		return err
	}
	defer log.Close()
	status := runstate.Status{State: "running", Run: runID, Card: c.Ref(), Title: c.Title, StartedAt: e.now().UTC().Format(time.RFC3339), LogPath: log.Path()}
	setStage := func(stage string) {
		status.Stage = stage
		status.Tokens = e.Budget.Snapshot().Cards[c.Ref()]
		_ = e.Paths.WriteStatus(status, e.now())
	}
	setStage("claim")
	defer func() {
		_ = e.Paths.WriteStatus(runstate.Status{State: "idle", Note: "last: " + c.Ref() + " " + status.Note}, e.now())
	}()
	_ = log.Write(runstate.Event{Kind: "start", Msg: "claimed " + c.Ref() + " on " + r.Info.Name})

	idem := func(what string) string { return runID + ":" + c.ID + ":" + what }
	if err := g.AddComment(ctx, c.ID, fmt.Sprintf("[AUTOPILOT CLAIMED — run %s] Working this card unattended. Outcome will be posted here.", runID), idem("claim")); err != nil {
		return fmt.Errorf("claim comment: %w", err)
	}
	if r.Working != "" {
		if err := g.MoveCard(ctx, c.ID, r.Working, idem("working")); err != nil {
			return fmt.Errorf("claim move: %w", err)
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, e.Cfg.RunTimeout.Duration)
	defer cancel()
	go e.watchStop(runCtx, cancel)

	wt, err := e.WT.Prepare(runCtx, e.repo(r.BC), c.Ref())
	if err != nil {
		return e.finish(ctx, g, r, c, log, &status, handoff.Run{ID: runID, CardKey: c.Ref(), Title: c.Title, Repo: r.BC.RepoName, When: e.now()},
			pipeline.Outcome{Kind: pipeline.Blocked, Summary: "could not prepare the worktree", Err: err}, idem)
	}
	run := handoff.Run{ID: runID, CardKey: c.Ref(), Title: c.Title, Repo: r.BC.RepoName, Branch: wt.Branch, LogPath: log.Path(), When: e.now()}
	if len(r.BC.Setup) > 0 {
		setStage("setup")
		// Setup commands come from the member's config and run before any
		// agent-written code exists in the worktree.
		res, err := CmdChecker{Dir: wt.Path, Checks: r.BC.Setup, Unsandboxed: true}.Run(runCtx)
		for _, s := range res {
			if !s.Passed && err == nil {
				err = fmt.Errorf("setup %q failed (exit %d)", s.Name, s.ExitCode)
			}
		}
		if err != nil {
			return e.finish(ctx, g, r, c, log, &status, run, pipeline.Outcome{Kind: pipeline.Blocked, Summary: "worktree setup failed", Err: err}, idem)
		}
	}
	agent, cleanup, err := e.NewAgent(runCtx, wt.Path, c, r.BC, []string{r.Done}, log, setStage)
	if err != nil {
		return e.finish(ctx, g, r, c, log, &status, run, pipeline.Outcome{Kind: pipeline.Blocked, Summary: "could not start opencode", Err: err}, idem)
	}
	defer cleanup()
	run.Base = r.BC.Base
	out := pipeline.Run(runCtx, cardContext(c, r), agent, WTWorkspace{WT: wt}, CmdChecker{Dir: wt.Path, Checks: r.BC.Checks, Sandbox: &Sandbox{Worktree: wt.Path, ReadOnly: r.BC.SandboxReadOnly}}, pipeline.Config{MaxRounds: e.Cfg.MaxRounds})
	if out.Kind == pipeline.ReadyForReview {
		setStage("publish")
		url, err := e.Pub.Publish(runCtx, wt.Path, wt.Branch, r.BC.Base, handoff.CommitMessage(run, out), handoff.PRTitle(run), handoff.PRBody(run, out))
		if err != nil {
			out.Kind, out.Summary, out.Err = pipeline.Blocked, "passed review but publishing failed", err
		} else {
			run.PRURL = url
			run.GitHubRepo = githubRepoFromPR(url)
			if d, err := worktree.Diff(runCtx, wt); err == nil {
				run.Diff = d
			}
		}
	}
	return e.finish(ctx, g, r, c, log, &status, run, out, idem)
}

// finish posts the outcome comment and moves the card. It uses the parent
// ctx so a run timeout still reports back.
func (e *Engine) finish(ctx context.Context, g *board.Guard, r Resolved, c board.Card, log *runstate.RunLog, status *runstate.Status, run handoff.Run, out pipeline.Outcome, idem func(string) string) error {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	var comment, list string
	switch out.Kind {
	case pipeline.ReadyForReview:
		comment, list = handoff.ReviewPacket(run, out), r.Review
	case pipeline.NeedsClarification:
		path, _ := handoff.WriteChatGPTPrompt(dirOf(log.Path()), run, out.Preflight)
		comment, list = handoff.ClarificationComment(run, out.Preflight, path), r.Clarify
	default:
		comment, list = handoff.BlockedComment(run, out), r.Blocked
	}
	status.Note = string(out.Kind)
	data := map[string]any{"kind": out.Kind, "summary": out.Summary, "rounds": len(out.Rounds), "pr": run.PRURL}
	if out.Err != nil {
		data["error"] = out.Err.Error()
	}
	_ = log.Write(runstate.Event{Kind: "end", Msg: out.Summary, Data: data})
	e.printf("%s: %s — %s %s\n", c.Ref(), out.Kind, out.Summary, run.PRURL)
	if err := g.AddComment(fctx, c.ID, comment, idem("outcome")); err != nil {
		return fmt.Errorf("outcome comment: %w", err)
	}
	if err := g.MoveCard(fctx, c.ID, list, idem("outcome-move")); err != nil {
		return fmt.Errorf("outcome move: %w", err)
	}
	return nil
}

func (e *Engine) watchStop(ctx context.Context, cancel context.CancelFunc) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if e.Paths.Stopped() {
				cancel()
				return
			}
		}
	}
}

// githubRepoFromPR turns https://github.com/o/r/pull/3 into o/r.
func githubRepoFromPR(u string) string {
	p := strings.TrimPrefix(u, "https://github.com/")
	parts := strings.Split(p, "/")
	if len(parts) >= 2 && parts[0] != "" {
		return parts[0] + "/" + parts[1]
	}
	return ""
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}
