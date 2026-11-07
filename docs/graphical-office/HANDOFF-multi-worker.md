# Handoff: multi-worker office (workers.json) → Codex

Branch `officeview/multi-worker`, stacked on draft PR #17 (`officeview/realistic-office`).
Read `CODEX.md` first and follow every hard rule. Do not merge anything without explicit Founder approval.

## Why this branch exists

Review of #17 against current `main` (6ee8584):

- **#17 applies cleanly.** Trial merge has 0 conflicts. The merged tree passes `go vet` and `go test -race` (11 packages) and the 220-check `office-proof.mjs`.
- **Neither `main` nor #17 reads `workers.json`.** The engine writes it (`runstate.WriteWorkers`), but `cmd/officeview` ignores it.
- **With two or more active lanes** `status.json` becomes `{state: running, note: "N lanes active; see workers.json"}` with no card, stage or log path. The old office could only show "Running · Stage unavailable". It never showed parallel work.
- **No `workers.json` exists on disk yet.** Every real run so far was single-lane, so there is no real parallel run to screenshot. All fixtures are TEST DATA.

## What this branch adds (truthfulness rules)

Server (`cmd/officeview/state.go`, `server.go`):

- Reads `workers.json` through the rooted reader (1 MiB cap). Strict parse: version 1, `workers` array, unique non-empty lane names, at most 8 lanes. Bad file means `workersError`, no lanes.
- **Pairing rule:** lanes are used only if `workers.json.updatedAt` equals `status.json.updatedAt`. The engine writes both in one call with one timestamp (`persistStatusesLocked`). Older means a later single-worker write, newer means an unpaired write; the lane data is ignored with a stated reason. State checks:
  - zero lanes need `idle`
  - one lane must be mirrored exactly by `status.json`
  - two or more need `status.json` to be the summary (running, no card)
- Reads each lane's own log (rooted, `runs/` only) and labels its events with the lane. The SSE stream follows every lane log with independent tail state.
- `lastRun` finished-history is no longer offered while `status.json` says running.
- Per-lane `cardTokens` from `usage.json` cards.

Client (`assets/app.js`, `index.html`, `style.css`), lane mode only when 2 or more lanes are usable:

- Lane list in the "Current card" panel (lane, card key, title, stage, round, reported tokens, freshness, "This card: X / cap", Inspect button).
- One **static** worker per lane at that lane's reported desk. No walking, no animation. Labels stack when lanes share a desk. Labels are desktop-only (≥1100px); the lane list and desk badges name lanes on phones.
- Desk badges name the working or done lane(s). The narrow desktop desks (claim, setup, pre-flight) show a short badge.
- Strip text lists every lane and its stage. The HUD card meter says "Per lane".
- Inspector shows one section per lane. Activity ticker rows are prefixed `[lane]`, ordered across logs by real timestamps.
- A visible warning appears when `status.json` says lanes are active but lane detail is missing or unpaired. No lanes are drawn in that case.

## Spec conflicts to flag to the Founder (do not hide)

`VISUAL_SPEC.md` says "one worker, one active card" and "no extra workers". Real parallel lanes need one worker per lane. This branch reuses the existing worker symbol, card chip and inspector, and adds no new style. It is only a draft PR; the Founder decides whether to accept the deviation.

## Verification state

Run from the repo root:

```sh
go vet ./cmd/officeview && go test -race -count=1 ./cmd/officeview ./internal/autopilot/...
go build -o /tmp/officeview-browser/officeview ./cmd/officeview
PLAYWRIGHT_MODULE=/tmp/officeview-browser/node_modules/playwright/index.mjs \
OFFICEVIEW=/tmp/officeview-browser/officeview OUT=docs/graphical-office/evidence \
  node cmd/officeview/office-proof.mjs
```

- Chrome is `/usr/bin/google-chrome` (the proof uses it; Playwright's own browser download is missing).
- Go: all `cmd/officeview` tests pass. A mutation check (forcing the pairing rule to "usable") makes `TestStaleOrUnpairedWorkersAreIgnoredWithReason` fail, so the tests have teeth.
- Browser proof: see the PR description for the final count. Before the last fixes it was 422 PASS and 2 FAIL, and both failures were a bug in the test itself (the stacked-label check also ran on the phone, where labels are intentionally hidden). That check is now desktop-only.

## Still to do (in order)

1. Re-run the full proof and confirm `ALL CHECKS PASSED`. Then copy the `office-lanes*` screenshots into `docs/graphical-office/evidence/` (they are written to the proof's `OUT` directory).
2. Look at the screenshots yourself, not only at the check results:
   - the phone lane list and desk badges (compact desks have not been eyeballed since the badge change)
   - the same-desk case (labels must be stacked, not overlapping)
   - dark mode
3. **Contract check against the real writer.** Merge `origin/main` into a scratch worktree of this branch and generate `workers.json` and `status.json` with `runstate.Paths.WriteWorkers` and `WriteStatus` exactly as `persistStatusesLocked` does. Confirm `/api/state` shows the lanes. The committed Go tests use hand-written JSON that mirrors the schema, so this is the one place a schema drift could hide.
4. Run `browser-proof.mjs` (Codex's older broad proof) on the merged tree. It passed on the #17+main trial merge but has not been run with these changes.
5. Document the lane telemetry in `docs/graphical-office/TELEMETRY.md` (pairing rule, lane logs, per-lane card usage, what stays Unknown) and add the results to `VERIFICATION.md`.
6. Open the draft PR against `officeview/realistic-office` (stacked on #17, so the diff stays small) with real output and screenshots. Say plainly that no real `workers.json` run was observed.
7. Optional after approval: instrumentation PR (stage events for claim/setup/checks/publish and structured guardrail/check results) so the Unknown desks become real. Never invent them in the UI.

## Do not

- Display parallel activity that `workers.json` does not prove. Idle means idle.
- Start or queue BII-833 or BII-830. FE is blocked by clean-history/source clearance and the missing FE baseline; BE is blocked by BII-504's file lease.
- Run live autopilot work today. The ledger already exceeds the daily cap, and STOP is currently set.
- Edit board cards, push to `main` or upstream, or merge #17 or this branch.
