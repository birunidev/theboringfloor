# Autopilot cockpit verification

7 November 2026. CODEX.md section 6 items 2 and 3. No merge or deployment.

## BII-812 live run

Changed in `birunidev/birunidev-space`: `backend/tests/unit/__init__.py` and
`backend/tests/unit/test_policy.py` (+189 lines). No application code changes.
Draft PR: https://github.com/birunidev/birunidev-space/pull/4

Command: `./bin/autopilot --live --once --board "V2 Engineering"`

The first run (`20261107T001138.679Z`) was interrupted by the Codex server
restart during review. Its checks passed, but two reviewers recorded
`interrupted: context canceled`; the engine moved the card to On Hold.
Codex recorded the recovery on the card and requeued it under the existing
founder authorization. No holds or policies were overridden.

Resumed run (`20261107T001349.827Z`), real stdout:

```text
BII-812: ready-for-review — passed all gates and 3 reviewers in round 1 https://github.com/birunidev/birunidev-space/pull/4
```

`get_card(BII-812)` confirmed Review (`list_4e3314cff769`) and the
`[AUTOPILOT REVIEW PACKET v1` comment. `gh pr view 4 --repo
birunidev/birunidev-space --json url,isDraft,baseRefName,headRefName,state,files`
returned OPEN, `isDraft:true`, `autopilot/BII-812` into `bii-504-slice-qa`,
and only the two files above.

Independent pytest rerun used `engine.Sandbox.Wrap`'s bubblewrap layout:
network/PID/IPC isolation, read-only filesystem, empty home and private tmp,
only the card worktree writable, uv Python and venv exposed read-only.
Command inside that sandbox:

```sh
/home/birunidev/.theboringfloor/autopilot/venvs/birunidev-space/bin/python -m pytest -q -p no:cacheprovider backend/tests --ignore=backend/tests/test_core_slice_local.py
```

Real output:

```text
......................                                                   [100%]
22 passed in 0.06s
```

`gh api repos/birunidev/birunidev-space/git/ref/heads/main --jq .object.sha`
returned `64abe00d730fc3d9e1c433cd783f68141712bf65` before and after.
Founder checkout HEAD stayed `d79518b30a5247a1964ddcc31cc1a66cf8b2a08d`.

**Open doubt:** the founder checkout started clean but ended with untracked
`.opencode/`. Its files have timestamps around 00:13:43 UTC, during the restart
recovery and before the resumed run began. Attribution is unproven. The files
were preserved. The checkout-untouched assertion therefore does not pass.

## TUI cockpit

Implementation: `internal/panels/autopilot.go`; app wiring in
`internal/app/model.go`, `workspace.go`, `keys.go`; tab/slash registration in
`internal/panels/tabs.go`, `popover.go`. Tests in `internal/panels/autopilot_test.go`
and `internal/app/autopilot_test.go`; existing cycle, digit and mouse tests
updated for the ninth tab.

Open with `/autopilot`, Tab, a tab-label click, or digit 9 outside chat input.
`s` sets/clears STOP, up/down scroll the retained 20 events. Polling every
three seconds also works while another tab is selected. Loading, missing,
corrupt and stale-day usage are handled explicitly. The cap comes from
`config.json`; `usage.json` supplies consumption. Missing cap reads unknown.
The panel never opens a writable Budget. Its only writes call SetStop/ClearStop.
Log reads stay under `runs/`, reject escaping symlinks, sanitize terminal control
characters and read a bounded 256 KiB tail. No model backend is required.

Verification commands:

```sh
go vet ./internal/autopilot/... ./cmd/autopilot/ ./internal/panels ./internal/app
go test -race -count=1 ./internal/autopilot/... ./internal/panels ./internal/app ./cmd/uishot
go test -race -count=1 ./internal/app ./internal/panels ./cmd/uishot
go build -o bin/theboringfloor ./cmd/theboringfloor
git diff --check
```

Vet, build and diff check exited 0 without output. The first regression run
identified three old eight-tab expectations; those tests now assert the ninth
tab, preserve the existing indexes, and test the new wrap/click behavior.
Real passing race output (autopilot from the first run; UI from the final run):

```text
ok github.com/theboringhumane/theboringfloor/internal/autopilot/board 1.014s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/engine 1.475s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/handoff 1.020s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/ocv2 1.088s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline 1.022s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/publish 1.263s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/runstate 1.017s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard 1.035s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/worktree 1.349s
ok github.com/theboringhumane/theboringfloor/internal/app 106.779s
ok github.com/theboringhumane/theboringfloor/internal/panels 10.632s
ok github.com/theboringhumane/theboringfloor/cmd/uishot 3.373s
```

`TestAutopilotWorkspaceshot` renders the actual app with isolated fixture files
in every registered theme at 40x18, 70x24, 140x40 and 220x60. It checks frame
dimensions, required labels, log visibility and unchanged-frame stability.
Panel tests additionally cover 1x1 and 24x8, log scrolling, yesterday's ledger,
corrupt/missing files, partial log records, outside/symlink log paths, and
byte-for-byte preservation of every file after refresh and both STOP actions.

Actual `tbo-v1` smoke command (automated PTY, 140x40):

```sh
THEFLOOR_HOME=/tmp/autopilot-cockpit-proof/home2 THEFLOOR_AUTOPILOT_HOME=/tmp/autopilot-cockpit-proof/state THEFLOOR_NO_NOTIFY=1 tbo-v1 --project /tmp/autopilot-cockpit-proof/project --autokill=15s
```

It used isolated fixture telemetry and the real opencode v1 backend without
sending a model prompt. Slash completion needs Enter to accept then Enter to
execute. An initial demo attempt was inconclusive because demo questions own
the input; a sandboxed live attempt could not start its local backend. The
final live launcher test ran outside the outer sandbox and reported:

```text
STOP at 6 seconds: True
STOP at 8 seconds: False
exit: 0 autopilot visible: True bytes: 8026
PASS: tbo-v1 live v1 launcher, slash navigation, expansion, STOP set and cleared
backend failed: False
poll fixture displayed: True
```

No open cockpit functional doubts found by these checks. This is a monitoring
panel; clearing STOP does not launch a worker or promise immediate resumption.
