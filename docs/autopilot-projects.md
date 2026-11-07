# Autopilot project CLI

Run from this repository to build, then use the absolute binary path from any folder:

```sh
go build -o bin/autopilot ./cmd/autopilot
./bin/autopilot project help
./bin/autopilot project list
./bin/autopilot project check birunidev-space
./bin/autopilot project run birunidev-space
./bin/autopilot project run birunidev-space --live --once
./bin/autopilot project run birunidev-lab --live --once
./bin/autopilot project run all --live
```

Existing `repo_name`, board name and exact board ID are accepted without migration.
`run` defaults to model pre-flight, which may spend tokens and writes local run
logs/usage but does not mutate the board, commit or push. `--live` explicitly
enables the existing implementation pipeline. `--once` requires live mode;
`--card KEY` is available only for one project's pre-flight.

All projects share one worker lock, STOP switch, daily token budget and status
file. `all` processes configured boards through the existing worker loop; it
does not start parallel workers. Existing stop/resume/status commands still work.

## Register another project

A project can live anywhere, including `~/Documents/projects`. It needs an
existing Git checkout with an `origin` remote and the intended base branch
available locally as `origin/<base>`. Select the branch containing the actual
code; if needed, fetch that branch explicitly before registration.

```sh
./bin/autopilot project add my-app \
  --repo "$HOME/Documents/projects/my-app" \
  --board board_REPLACE_ME \
  --base main \
  --policy BII-REPLACE_ME \
  --check '["go","test","./..."]' \
  --preview
```

Replace the example board, policy card and check command with this project's
real values. Remove `--preview` to save after validation. Repeat `--policy` in
priority order for READ_FIRST cards and `--check` for each verification command.
Checks are JSON argument arrays executed directly in bubblewrap; shell strings
are not parsed. Dependencies must already be available: registration never
installs packages or infers commands from card prose. Use repeatable
`--read-only PATH` for narrow dependency directories exposed to checks. Projects
needing setup steps or custom check names can use the existing config fields;
this CLI does not generate setup commands.

The board must have an `Autopilot Queue`, `Review`, `On Hold` and `Done` list,
plus a policy description or explicit policy cards. `Needs Clarification` falls
back to `On Hold` when absent. Override list names with `--queue`, `--review`,
`--blocked`, `--clarify`, and optional `--working`. Done routing and ambiguous
list names are rejected. Project aliases use letters, digits, hyphens or
underscores; `all` is reserved. Duplicate repositories and boards are rejected.

Registration atomically saves `~/.theboringfloor/autopilot/config.json` with mode
0600, preserving existing projects and global settings. It refuses to overwrite
concurrent config edits or save while a worker holds the lock. Exit the worker
normally before saving, then restart it to load the new configuration. Preview,
list and check can run while the worker is active. Registration does not change
STOP, token usage, status, card text or board lists.

## What check means

`project check NAME` reads Git and board metadata, resolves policy cards and list
routes, checks configured dependency paths, and looks for `opencode`, `bwrap` and
`gh` on PATH. It does not execute check commands, start a model, verify GitHub
authentication, or approve card scope. Space uses the existing pinned MCP client
and OAuth login; its token source may refresh credentials as usual.

A clear board is still subject to pre-flight and founder holds, including
security and release gates. Live execution retains the existing ask-everything
permissions, sandboxed checks, STOP/budget limits, forbidden Done moves and
branch-only draft PR publication. No bypass option is provided.

Every project command accepts `--config PATH` after its subcommand/project name.
This selects configuration, while worker state and lock remain in the standard
autopilot home. The cockpit and graphical UI continue reading that shared state.
