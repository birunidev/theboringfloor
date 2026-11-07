# Graphical Office telemetry

`cmd/officeview` is a local file reader and embedded browser view. It does not
connect to Space, start opencode, run checks, change cards, push, merge or deploy.
It uses the attached VISUAL_SPEC.md verbatim; no mockup PNGs were supplied.

## Sources

| Source | Mapping |
|---|---|
| `status.json` | Literal state/card/title/stage/note/update time; optional integer round and reported tokens. Idle overrides retained card/stage for the active view. |
| `usage.json` | Valid daily total and matching current-card lifetime total. Missing, negative, oversized or structurally invalid numbers remain unavailable. |
| `config.json` | Only daily/card caps and configured board names enter the response. Missing caps default to 10,000,000 and 2,000,000 as specified; supplied invalid caps remain unavailable. |
| `STOP` | Independent set/clear/unknown fact. Never inferred from a cap, permission rejection, error or terminal event. |
| `status.logPath` | Must resolve within the rooted `runs/` directory. Native event objects are delivered with derived file-identity/byte-offset IDs. |
| `runs/*/*.jsonl` | At most 20 recent run names, literal card directory names and file modification times. Not board-list membership. |

The adapter mirrors `Budget.Snapshot`'s **server-local** daily rollover in
memory: a valid previous-day ledger total displays zero for today, without
writing the ledger. Invalid/future ledger days display unavailable. Ledger day
and server timezone are disclosed; the browser does not reset days itself.
`status.tokens` is separately labeled **Reported tokens**, never substituted
for either usage meter.

## Evidence and attribution

An explicit status run/card pair is required for current-run enrichment. If
status has no run ID, only the current log identity plus a confirmed `start`
boundary and matching card permits attribution. New run identities reset desk
results. Old-card events remain literal activity, without moving the worker or
coloring current desks. A newer active snapshot invalidates an older terminal
display. Idle snapshots always place the worker at Queue without a carried card.

`stage-done` means Done for the two model stages that remain: pre-flight and
implementation. The coding-only worker has no reviewer stages. External review
by the founder, ChatGPT or CTO happens after the draft PR reaches Review and is
not depicted as live office work. There is no structured live contract for
sandboxed checks or deterministic gate results, and no checks-execution stage.
Therefore checks and gate desks remain Unknown. Legacy reviewer records remain
literal Activity history but never activate a worker or gate desk.

Attributable `end` events populate session shelves, deduplicated by run/card.
They do not claim board membership or set STOP. Native PR URLs are accepted only
as HTTP(S) URLs without credentials and open only on deliberate activation.
Outcome titles absent from native data/current matching status say unavailable.

## Transport and bounds

- `GET /api/state`: merged snapshot, availability flags, current log records,
  configured board names and recent run metadata. Rooted reads reject escaping
  symlinks; no endpoint serves arbitrary files or the configuration itself.
- `GET /api/events`: SSE follows the current log path and inode, retains partial
  records until newline, and follows a new file/run without restarting the page.
  Reconnects can replay records; the client deduplicates stable offset IDs and
  treats initial history as history rather than replaying work animations.
- State/config/usage files are limited to 1 MiB. Log reads use 4 MiB chunks;
  initial snapshots retain up to 500 recent records, and the browser retains
  up to 1,000 actual events, revealed five initially then 20 per disclosure.
  Oversized/malformed records produce an explicit activity error.
- Updates older than 60 seconds, invalid/future times, hidden documents, missing
  stages, disconnections, STOP and motion preferences freeze motion. Initial
  load and multi-stage jumps place the worker directly. Observed adjacent
  transitions use aisles; retries return directly without inventing stages.

## Controls

Default mode is read-only. `--controls` enables `POST /api/stop` and
`POST /api/resume` through `runstate.Paths.SetStop/ClearStop`. Every POST requires
the exact loopback Origin and an unpredictable process-local token in
`X-Office-Token`. Host checks also prevent alternate-host access. STOP symlinks
are rejected. Both actions require the specified accessible confirmation dialog.
The response contains a fresh file readback; an HTTP success alone does not
optimistically change STOP or animate resumption. These controls affect STOP
only and do not promise immediate interruption or start a worker.

All browser text uses textContent/native text nodes, including titles, notes,
errors, event payloads and configuration-derived names. The CSP disallows
external scripts, connections and embedding. Theme/motion preferences use local
browser storage as specified. `--test-data` only adds the visible TEST DATA
label; it never supplies fallback telemetry or changes the state mapping.

## Office readability additions (founder-approved, 7 Nov 2026)

The later founder decision moved review outside the autopilot. It supersedes the
original reviewer-desk model in `VISUAL_SPEC.md`. The office now shows the
coding sprint only: pre-flight, implementation, sandboxed checks, deterministic
gates, publish and outcome. Every live value below is read from existing
telemetry; nothing new is inferred.

| What the office now shows | Exact source |
|---|---|
| Office status strip (Working / Between reported stages / Stopped / Error reported / Ready for review / Questions asked / On hold / Idle) | `status.json` state+stage+updatedAt, STOP, current-run `error` and `end` records; precedence in `officeState()` |
| Who handles a desk | Stage → role as the engine assigns it: `preflight` Pre-flight agent and `implement:N` Implementer; claim/setup/checks/gates/publish Autopilot engine (no model) |
| Desk facts `✓ allowed · × rejected · tokens` | Current-run `permission` records carry `stage` and `data.allow`; `stage-done.data.billable` |
| Between reported stages | The status stage already has `stage-done` and no new stage started. Checks and publish emit no log records today, so they appear here, never as Working |
| Inspector (dialog) | Literal `stage.msg` (session), `stage.ts`, `stage-done.ts`, `billable`, last 10 `permission` records with literal reasons, `stage-done.msg` labeled "Agent's own summary · not verified". Missing fields say Unavailable and name the missing record |
| Idle activity | `lastRun`: the most recently modified `runs/*/*.jsonl` when `status.json` names no log; static history, no animation, never attributed to the current status |
| Outcome shelves | `todayOutcomes`: `end` records dated today (server-local) from run logs modified today, deduplicated by run/card; labeled "Today, from run logs"; not board membership |

Still Unknown until the engine instrumentation PR: checks execution and
results, deterministic scope/sensitive/diff gate results, claim/setup/publish
timing and sessions, failure reason (gate failure vs error vs cap), outcome card
titles, and the queue.

## Parallel lanes (`workers.json`)

With two or more active lanes the engine writes `status.json` as a summary
(`state: running`, no card/stage/log path, note "N lanes active; see
workers.json"). Each lane's card, stage, round and log path exist only in
`workers.json`. The office reads it as follows; nothing is inferred.

| Rule | Detail |
|---|---|
| Absent file | No lane data, no error. "Absence means no parallel-lane run has been recorded." |
| Parse | Rooted read, 1 MiB cap. Version must be 1, `workers` an array, at most 8 lanes, lane names unique and non-empty. Anything else is `workersError`, no lanes. |
| Pairing | Lanes are used only when `workers.json.updatedAt` equals `status.json.updatedAt`. `Engine.persistStatusesLocked` writes both in one call with one timestamp. Older means a later single-worker write; newer means an unpaired write. Both are ignored with a stated reason, so a crash can never leave ghost lanes. |
| State consistency | Zero lanes require `idle`; one lane must be mirrored exactly by `status.json` (state, run, card); two or more require the summary (`running`, no card). |
| Lane logs | Each lane's `logPath` must resolve inside `runs/` (rooted; escaping paths and symlinks are refused per lane). Records are labelled with the lane, streamed over `/api/events`, and attributed to a lane only by its own log plus the run and card it reports now. |
| Card usage | Per-lane lifetime card tokens from `usage.json` `cards`, shown against the configured per-card cap. Unknown shows "usage unavailable". |
| Lane mode | Two or more usable lanes. One static worker per lane at its last reported desk (no walking or animation), lane list, lane-aware desk badges, per-lane inspector sections. One lane is already mirrored by `status.json`, so the single-worker view is unchanged apart from lane-labelled activity. |
| Missing detail | `status.json` says lanes are active but detail is absent, unpaired or unreadable: a visible warning with the reason; no lanes drawn. |
| Finished history | "Last run" history is never offered while `status.json` says running. |

Still Unknown: checks, guardrail results, per-lane queue, failure reasons. They
need the instrumentation PR.
