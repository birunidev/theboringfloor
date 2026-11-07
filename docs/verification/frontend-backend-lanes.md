# Frontend/backend lane verification (7 November 2026)

## Board context read

Read-only Birunidev Space calls inspected V2 Engineering board
`board_938353e08e72` and its three relevant lists:

- Autopilot Queue `list_4f9acb16db23`: empty.
- Frontend In Progress `list_103e3a15d696`: empty.
- Backend In Progress `list_83302fa3db91`: BII-504 only.

The board description defines the flow as Todo, separate FE/BE In Progress,
Integration & QA, external Review, then Done only with Founder approval. BII-504
explicitly says `ACTIVE_OWNER` and `NOT QUEUEABLE`; it was not changed or
dispatched. No board write tool was called.

## Changed behavior

- `--live --parallel` opts into one concurrent card per configured lane.
- Title-prefix routing is anchored, case-insensitive and configured. Empty,
  overlapping, ambiguous or unmatched routing fails before board writes.
- Each lane resolves its own non-Done working list; the existing Guard still
  forbids every move to Done.
- One process/global lock owns all lanes. The shared budget is mutex-protected;
  STOP cancels each run. Board calls and same-repository worktree preparation
  are serialized; model/check work runs in separate per-card worktrees.
- A claim re-read must still show the card in Autopilot Queue.
- `workers.json` reports every active lane atomically while `status.json`
  remains backward-compatible.

## Verification commands and real output

Required isolated suite:

```sh
bwrap --die-with-parent --unshare-net --ro-bind / / --dev /dev --proc /proc \
  --tmpfs /tmp --tmpfs /home/birunidev \
  --ro-bind /tmp/theboringfloor-lanes /tmp/theboringfloor-lanes \
  --ro-bind /home/birunidev/go/pkg/mod /home/birunidev/go/pkg/mod \
  --chdir /tmp/theboringfloor-lanes --setenv HOME /home/birunidev \
  --setenv GOCACHE /tmp/go-cache --setenv GOMODCACHE /home/birunidev/go/pkg/mod \
  --setenv GOPROXY off -- /usr/bin/sh -c \
  'go vet ./internal/autopilot/... ./cmd/autopilot/ && \
   go test -race -count=1 ./internal/autopilot/... ./cmd/autopilot/'
```

Vet exited 0 without diagnostics. Actual test output:

```text
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/board      1.020s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/engine     1.598s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/handoff    1.020s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/ocv2       1.103s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline   1.018s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/publish    1.272s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/runstate   1.022s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard 1.049s
ok  github.com/theboringhumane/theboringfloor/internal/autopilot/worktree   1.373s
?   github.com/theboringhumane/theboringfloor/cmd/autopilot                 [no test files]
```

The new race test blocks both fake agents at pre-flight and proves both are
active together. While blocked, `workers.json` contains sorted backend and
frontend entries and legacy status says two lanes are active. It then proves
the cards visited their distinct working lists, ended outside Done, and the
batch completed. A separate test proves unmatched routing performs zero moves
and zero comments. Config tests cover overlapping prefixes and Done routing.

This full run includes the same-repository preparation lock and ready-list
recheck. `gofmt` and `git diff --check` exited 0 without output.

The CLI was also built without network access to `/tmp/autopilot-lanes`.
`/tmp/autopilot-lanes --help` exited 0 and listed:

```text
-parallel
    live: work at most one card per configured lane concurrently
```

## Limits

No live worker, model, check command, branch push or card mutation was run. The
real config is unchanged, so parallel mode is not active. It must be reviewed,
merged, configured with the board's exact lane prefixes/lists, then the running
loop must be restarted explicitly. The graphical office correction in draft
PR #17 remains truthful to the current single-status runtime; a later adapter
can render `workers.json` after this contract is accepted.
