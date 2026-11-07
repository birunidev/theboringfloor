// autopilot — unattended card worker for Birunidev Space boards.
//
//	autopilot --dry-run [--board NAME] [--card BII-123]   pre-flight only, no board writes, no push
//	autopilot --live --once                               work at most one card, then exit
//	autopilot --live                                      loop forever (poll interval from config)
//	autopilot stop "reason" | resume | status
//
// Config: ~/.theboringfloor/autopilot/config.json (see internal/autopilot/engine).
// Hard rules live in code: branch + draft PR only, never main, never Done,
// never merge or deploy, every opencode action approved by an allowlist.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/engine"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/ocv2"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/publish"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/worktree"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "autopilot:", err)
		os.Exit(1)
	}
}

func run() error {
	return runArgs(os.Args[1:])
}

func runArgs(args []string) error {
	paths := runstate.Paths{Root: runstate.DefaultRoot()}
	if len(args) > 0 {
		switch args[0] {
		case "project":
			workerArgs, err := runProjectCommand(paths, args[1:])
			if err != nil || workerArgs == nil {
				return err
			}
			args = workerArgs
		case "stop":
			reason := strings.Join(args[1:], " ")
			if reason == "" {
				reason = "manual stop"
			}
			if err := paths.SetStop(reason, time.Now()); err != nil {
				return err
			}
			fmt.Println("kill switch set:", paths.StopFile())
			return nil
		case "resume":
			if err := paths.ClearStop(); err != nil {
				return err
			}
			fmt.Println("kill switch cleared")
			return nil
		case "status":
			s, err := paths.ReadStatus()
			if err != nil {
				return err
			}
			fmt.Printf("state=%s card=%s stage=%s tokens=%d stopped=%v updated=%s %s\n", s.State, s.Card, s.Stage, s.Tokens, paths.Stopped(), s.UpdatedAt, s.Note)
			return nil
		}
	}

	flags := flag.NewFlagSet("autopilot", flag.ContinueOnError)
	cfgPath := flags.String("config", filepath.Join(paths.Root, "config.json"), "config file")
	dry := flags.Bool("dry-run", false, "pre-flight only; no board writes, no commits, no push")
	live := flags.Bool("live", false, "allow board writes, commits, push and draft PRs")
	once := flags.Bool("once", false, "live: work at most one card, then exit")
	cardRef := flags.String("card", "", "dry-run: this card only (label not required)")
	boardName := flags.String("board", "", "only this board (name or id)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments; use 'autopilot project help' for project commands")
	}
	if *dry == *live {
		return errors.New("pass exactly one of --dry-run or --live")
	}
	if *cardRef != "" && !*dry {
		return errors.New("--card is for --dry-run only; live runs pick labelled cards")
	}

	cfg, err := engine.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if *boardName != "" {
		var keep []engine.BoardConfig
		for _, b := range cfg.Boards {
			if strings.EqualFold(b.Name, *boardName) || b.BoardID == *boardName {
				keep = append(keep, b)
			}
		}
		if len(keep) == 0 {
			return fmt.Errorf("no board %q in config", *boardName)
		}
		cfg.Boards = keep
	}
	home, _ := os.UserHomeDir()
	if err := ocv2.CheckSpaceConfig(home); err != nil {
		return fmt.Errorf("space config check: %w", err)
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	lock, err := runstate.AcquireLock(paths.LockFile())
	if err != nil {
		return err
	}
	defer lock.Release()
	budget, err := runstate.OpenBudget(paths.UsageFile(), cfg.PerCardTokens, cfg.DailyTokens, nil)
	if err != nil {
		return err
	}
	model, err := ocv2.ParseModel(cfg.Model)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	space := spaceboard.New(spaceboard.NewTokenSource(spaceboard.DefaultAuthFile(), ""))
	e := &engine.Engine{
		Cfg: cfg, Paths: paths, Space: space, Budget: budget,
		WT:  worktree.Manager{Root: paths.WorktreesDir(), Fetch: true},
		Pub: gitPublisher{},
		Out: os.Stdout,
		NewAgent: func(ctx context.Context, dir string, c board.Card, bc engine.BoardConfig, done []string, log *runstate.RunLog, onStage func(string)) (pipeline.Agent, func(), error) {
			srv, err := ocv2.Spawn(ctx, dir)
			if err != nil {
				return nil, nil, err
			}
			a := &engine.OCAgent{Client: srv.Client(), Dir: dir, Model: model, Cfg: cfg, Checks: bc.Checks,
				DoneListIDs: done, Card: c.Ref(), Budget: budget, Log: log, OnStage: onStage}
			return a, func() { srv.Close() }, nil
		},
	}

	if *dry {
		for _, bc := range cfg.Boards {
			fmt.Printf("\n=== DRY RUN: %s (%s) ===\n", bc.Name, bc.RepoName)
			results, err := e.DryRun(ctx, bc, *cardRef)
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			if len(results) == 0 {
				fmt.Println("no eligible cards")
			}
			for _, r := range results {
				printDry(r)
			}
		}
		u := budget.Snapshot()
		fmt.Printf("\ntokens today: %d\n", u.Today)
		return nil
	}

	for {
		err := e.Tick(ctx)
		switch {
		case errors.Is(err, runstate.ErrStopped):
			fmt.Println("kill switch is set; exiting")
			return nil
		case errors.Is(err, engine.ErrNothingToDo):
			if *once {
				fmt.Println("no eligible card")
				return nil
			}
		case err != nil:
			fmt.Println("tick:", err)
			if *once {
				return err
			}
		default:
			if *once {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.Poll.Duration):
		}
	}
}

func printDry(r engine.DryRunResult) {
	fmt.Printf("\n--- %s  %s\n", r.Card.Ref(), r.Card.Title)
	if r.Err != nil {
		fmt.Printf("    ERROR: %v\n", r.Err)
		return
	}
	p := r.Preflight
	fmt.Printf("    ready: %v   would move to: %s\n", p.Ready, r.WouldList)
	if p.PolicyBlock != "" {
		fmt.Printf("    policy block: %s\n", p.PolicyBlock)
	}
	if p.Summary != "" {
		fmt.Printf("    summary: %s\n", p.Summary)
	}
	for _, ac := range p.AcceptanceCriteria {
		fmt.Printf("    AC: %s\n", ac)
	}
	if len(p.ScopePaths) > 0 {
		fmt.Printf("    scope: %s\n", strings.Join(p.ScopePaths, ", "))
	}
	for _, q := range p.Questions {
		fmt.Printf("    Q: %s\n", q)
	}
	if !p.Ready {
		fmt.Printf("\n    would comment:\n%s\n", indent(r.Would, "      | "))
	}
}

func indent(s, p string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = p + lines[i]
	}
	return strings.Join(lines, "\n")
}

// gitPublisher commits, pushes the autopilot branch and opens a draft PR.
type gitPublisher struct{}

func (gitPublisher) Publish(ctx context.Context, dir, branch, base, msg, title, body string) (string, error) {
	t := publish.Target{Dir: dir, Branch: branch, Base: base}
	if _, _, err := publish.Commit(ctx, t, msg); err != nil {
		return "", err
	}
	if err := publish.Push(ctx, t); err != nil {
		return "", err
	}
	url, _, err := publish.DraftPR(ctx, t, title, body)
	return url, err
}
