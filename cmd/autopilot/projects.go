package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/engine"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/projects"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
	"github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard"
)

type repeated []string

func (r *repeated) String() string         { return strings.Join(*r, ",") }
func (r *repeated) Set(value string) error { *r = append(*r, value); return nil }

const projectHelp = `Usage:
  autopilot project list
  autopilot project add NAME --repo PATH --board BOARD_ID --base BRANCH --check '["program","arg"]'
  autopilot project check NAME
  autopilot project run NAME [--live] [--once] [--card CARD] [--ui-port 8080]
  autopilot project run all [--live] [--once] [--ui-port 8080]

Repeat --check for each sandboxed verification command and --policy for READ_FIRST cards.
Add also accepts --read-only dependency paths, --queue, --working, --review,
--blocked, --clarify and --preview (validate without saving).
All commands accept --config PATH after the subcommand/name.
Existing project names, repository names and board IDs work without migration.

check only validates Git/board metadata and dependencies; it does not approve work.
run defaults to model pre-flight only (may spend tokens); --live permits card work.
All runs share the existing single-instance lock, token ledger, STOP and policies.
run prints the UI address (default port 8080); start officeview separately.
`

// Returns legacy worker flags only for run. Registration and metadata checks
// cannot enter the worker loop or call any board mutation operation.
func projectCommand(ctx context.Context, paths runstate.Paths, args []string, out io.Writer, api func() engine.SpaceAPI) ([]string, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, projectHelp)
		return nil, nil
	}
	command := args[0]
	args = args[1:]
	if command != "add" && command != "list" && command != "check" && command != "run" {
		return nil, fmt.Errorf("unknown project command %q; use 'autopilot project help'", command)
	}
	name := ""
	if command != "list" {
		if len(args) == 0 {
			return nil, errors.New("project name is required; use 'autopilot project help'")
		}
		if args[0] == "--help" || args[0] == "-h" {
			fmt.Fprint(out, projectHelp)
			return nil, nil
		}
		name, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("project "+command, flag.ContinueOnError)
	fs.SetOutput(out)
	configPath := fs.String("config", filepath.Join(paths.Root, "config.json"), "autopilot configuration")
	var repo, boardID, base, queue, working, review, blocked, clarify string
	var checks, policy, readOnly repeated
	var preview, live, once bool
	var card string
	var uiPort int
	if command == "add" {
		fs.StringVar(&repo, "repo", "", "existing repository root")
		fs.StringVar(&boardID, "board", "", "Birunidev Space board ID")
		fs.StringVar(&base, "base", "", "explicit origin base branch")
		fs.StringVar(&queue, "queue", "Autopilot Queue", "dedicated eligible-card list")
		fs.StringVar(&working, "working", "", "optional working list")
		fs.StringVar(&review, "review", "Review", "review list")
		fs.StringVar(&blocked, "blocked", "On Hold", "blocked list")
		fs.StringVar(&clarify, "clarify", "Needs Clarification", "clarification list; falls back to blocked")
		fs.Var(&checks, "check", "JSON argv array; repeat for multiple checks (never a shell string)")
		fs.Var(&policy, "policy", "policy card key/ID; repeat in priority order")
		fs.Var(&readOnly, "read-only", "dependency path exposed read-only to sandboxed checks")
		fs.BoolVar(&preview, "preview", false, "validate mapping without saving it")
	}
	if command == "run" {
		fs.IntVar(&uiPort, "ui-port", 8080, "port of the separately running localhost officeview UI")
		fs.BoolVar(&live, "live", false, "allow board writes, commits, push and draft PRs")
		fs.BoolVar(&once, "once", false, "live: process at most one card")
		fs.StringVar(&card, "card", "", "pre-flight: inspect this card only")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, nil
		}
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, errors.New("unexpected arguments; put flags after the project name")
	}
	if command == "run" && ((once && !live) || (live && card != "") || (name == "all" && card != "")) {
		return nil, errors.New("--once requires --live; --card requires pre-flight for one named project")
	}
	if command == "run" && (uiPort < 1 || uiPort > 65535) {
		return nil, errors.New("--ui-port must be between 1 and 65535")
	}
	doc, err := projects.Load(*configPath)
	if err != nil {
		return nil, err
	}
	if command == "list" {
		if len(doc.Config.Boards) == 0 {
			fmt.Fprintln(out, "No projects registered. Use 'autopilot project add'.")
			return nil, nil
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PROJECT / BOARD\tREPOSITORY\tBASE\tCHECKS\tPATH")
		for _, b := range doc.Config.Boards {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", display(b.Name), display(b.RepoName), display(b.Base), len(b.Checks), display(b.RepoDir))
		}
		return nil, w.Flush()
	}
	if command == "add" {
		b := engine.BoardConfig{Name: name, RepoName: name, RepoDir: repo, BoardID: boardID, Base: base, ReadyList: queue, WorkingList: working, ReviewList: review, BlockedList: blocked, ClarifyList: clarify, DoneList: "Done", Label: "none", PolicyCards: policy}
		for i, raw := range checks {
			var argv []string
			if json.Unmarshal([]byte(raw), &argv) != nil {
				return nil, errors.New("--check must be a JSON argv array, for example '[\"go\",\"test\",\"./...\"]'")
			}
			b.Checks = append(b.Checks, engine.Check{Name: fmt.Sprintf("check %d", i+1), Argv: argv})
		}
		if err := projects.ValidateMapping(b); err != nil {
			return nil, err
		}
		b.RepoDir, err = projects.ValidateRepo(ctx, repo, base)
		if err != nil {
			return nil, err
		}
		b.SandboxReadOnly, err = projects.ValidateReadOnly(readOnly)
		if err != nil {
			return nil, err
		}
		next, err := projects.Append(doc.Config, b)
		if err != nil {
			return nil, err
		}
		r, err := projects.ValidateBoard(ctx, api(), b)
		if err != nil {
			return nil, err
		}
		printProject(out, b, r)
		if preview {
			fmt.Fprintln(out, "Preview only; configuration and board were not changed.")
			return nil, nil
		}
		if err := projects.Save(*configPath, paths.LockFile(), doc, next); err != nil {
			return nil, err
		}
		fmt.Fprintf(out, "Registered %s. Next: autopilot project check %s\nThen preview: autopilot project run %s\n", name, name, name)
		return nil, nil
	}
	var selected []engine.BoardConfig
	if command == "run" && name == "all" {
		selected = doc.Config.Boards
		if len(selected) == 0 {
			return nil, errors.New("no projects registered")
		}
	} else {
		b, err := projects.Select(doc.Config, name)
		if err != nil {
			return nil, err
		}
		selected = []engine.BoardConfig{b}
	}
	for _, b := range selected {
		if err := projects.ValidateChecks(b); err != nil {
			return nil, err
		}
		if _, err := projects.ValidateRepo(ctx, b.RepoDir, b.Base); err != nil {
			return nil, fmt.Errorf("project %s: %w", b.Name, err)
		}
		if _, err := projects.ValidateReadOnly(b.SandboxReadOnly); err != nil {
			return nil, err
		}
		if command == "check" {
			for _, tool := range []string{"opencode", "bwrap", "gh"} {
				if _, err := exec.LookPath(tool); err != nil {
					return nil, fmt.Errorf("required tool %s is not on PATH", tool)
				}
			}
			r, err := projects.ValidateBoard(ctx, api(), b)
			if err != nil {
				return nil, err
			}
			printProject(out, b, r)
		}
	}
	if command == "check" {
		fmt.Fprintln(out, "Metadata ready. Commands were not executed. Card authorization and founder holds are still checked by pre-flight.")
		return nil, nil
	}
	workerArgs := []string{"--config", *configPath}
	if len(selected) == 1 {
		workerArgs = append(workerArgs, "--board", selected[0].BoardID)
	}
	if live {
		workerArgs = append(workerArgs, "--live")
		if once {
			workerArgs = append(workerArgs, "--once")
		}
		fmt.Fprintln(out, "Live mode: existing sandbox, policy, STOP, budget and draft-PR guards apply.")
	} else {
		workerArgs = append(workerArgs, "--dry-run")
		fmt.Fprintln(out, "Pre-flight only: may spend model tokens; no board writes, commits or pushes. Use --live explicitly for implementation.")
	}
	if card != "" {
		workerArgs = append(workerArgs, "--card", card)
	}
	fmt.Fprintf(out, "UI: http://127.0.0.1:%d (officeview must be running separately)\n", uiPort)
	return workerArgs, nil
}

func printProject(out io.Writer, b engine.BoardConfig, r engine.Resolved) {
	fmt.Fprintf(out, "Project: %s\nBoard: %s (%s)\nRepository: %s\nBase: origin/%s\nQueue: %s\nSandboxed checks: %d; policy cards: %d\n", display(b.Name), display(r.Info.Name), display(b.BoardID), display(b.RepoDir), display(b.Base), display(b.ReadyList), len(b.Checks), len(b.PolicyCards))
	if r.ClarifyFallback {
		fmt.Fprintf(out, "Clarification list absent; engine routes clarification to %s.\n", b.BlockedList)
	}
}

func display(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

func projectAPI() engine.SpaceAPI {
	return spaceboard.New(spaceboard.NewTokenSource(spaceboard.DefaultAuthFile(), ""))
}

func runProjectCommand(paths runstate.Paths, args []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return projectCommand(ctx, paths, args, os.Stdout, projectAPI)
}
