# CODEX.md — handoff for the theboringfloor autopilot

You are continuing work on an **unattended coding autopilot** that works the
founder's Birunidev Space board cards 24/7, with the theboringfloor TUI as the
monitoring cockpit. This is a **personal proof of concept** for the founder
(GitHub `birunidev`), not a Birunidev Space product feature.

Read this whole file before changing anything. Hard rules (section 4) are not
negotiable.

---

## 1. Where things are

| What | Where |
|---|---|
| This repo (fork) | `github.com/birunidev/theboringfloor`, local `~/Documents/projects/theboringfloor` |
| Upstream (never PR there) | `github.com/theboringhumane/theboringfloor` |
| Autopilot code | `internal/autopilot/*`, `cmd/autopilot` |
| Autopilot binary | `bin/autopilot` (git-ignored; `go build -o bin/autopilot ./cmd/autopilot`) |
| Autopilot home | `~/.theboringfloor/autopilot/` (config, logs, worktrees, venvs, status) |
| Config | `~/.theboringfloor/autopilot/config.json` (0600) |
| Run logs | `~/.theboringfloor/autopilot/runs/<CARD>/<run>.jsonl` |
| Live status (for the TUI) | `~/.theboringfloor/autopilot/status.json` |
| Token ledger | `~/.theboringfloor/autopilot/usage.json` |
| Kill switch | `~/.theboringfloor/autopilot/STOP` (`bin/autopilot stop|resume|status`) |
| Card worktrees | `~/.theboringfloor/autopilot/worktrees/<repo>/<KEY>` |
| Space backend venv for checks | `~/.theboringfloor/autopilot/venvs/birunidev-space` (built from uv Python 3.12) |
| Target repos (founder's checkouts, used only as git object stores) | `~/Documents/projects/birunidev-official/birunidev-space`, `.../birunidev-lab` |

**Local-only:** opencode, the Space login, the venv, bwrap and the founder's
checkouts exist only on the founder's Fedora machine. If you run in a cloud
sandbox you can edit and test the Go code (`go test ./internal/autopilot/...`),
but you cannot run the autopilot itself, live tests, or anything touching the
boards. Say so instead of pretending.

---

## 2. What exists (all merged to fork `main`)

| Fork PR | Package | What it does |
|---|---|---|
| #1 | `board` | Board interface; `Guard` refuses any move into a forbidden (Done) list; in-memory `Fake` |
| #2 | `runstate` | Paths, STOP kill switch, per-card + daily token caps (persisted), flock single-instance lock, JSONL run logs, `status.json` |
| #3 | `worktree` | Per-card worktree on `autopilot/<KEY>` from `origin/<base>`, no upstream tracking, resume, `CheckBranch`, `ChangedFiles`/`Diff`, `PrepareDetached` (dry runs) |
| #4 | `ocv2` | Client for **opencode 2.x** `/api/*`: private `opencode serve` (random password, own process group), sessions created **ask-everything**, `Policy` decides each permission in code (fails closed), `RunTurn` with token-cap/timeout interrupt, `CheckSpaceConfig` |
| #5 | `pipeline` | Pre-flight (strict JSON) → implementer → code gates + 3 parallel reviewers (acceptance criteria, tests/lint/build, diff scope) → max 3 rounds; verdicts fail closed |
| #7 | `publish`, `handoff` | Commit (hooks run), push only `HEAD:refs/heads/autopilot/*` (never forced), DRAFT PR via `gh` (or reuse); card comments (no command output) |
| #8 | `spaceboard`, `engine`, `cmd/autopilot` | Space MCP client pinned to `https://space.birunidev.com/mcp` reusing opencode v1's OAuth login (refresh + write-back); engine: resolve lists, pick, claim, worktree, pipeline, act; `--dry-run` / `--live [--once]` |
| #9 | `engine` | Select cards by a dedicated **"Autopilot Queue"** list (`"label": "none"`), because the Space MCP cannot create labels |
| #10 | `engine` | Checks run in **bubblewrap**: fs read-only, `$HOME` = empty tmpfs, only worktree writable + configured read-only paths, no network. Implementer can no longer run checks itself |
| #11 | `handoff`, `pipeline` | Standardized **`[AUTOPILOT REVIEW PACKET v1`** comment (incl. diff) for cards in Review; prompts honor `[CHATGPT REVIEW v1]` REQUEST_CHANGES findings |

Commands (from repo root):
```sh
./bin/autopilot --dry-run [--board "V2 Engineering"] [--card BII-123]   # pre-flight only; board writes disabled
./bin/autopilot --live --once [--board NAME]                            # work at most one queued card
./bin/autopilot --live                                                  # loop; poll interval from config
./bin/autopilot stop "reason" | resume | status
```

Proven so far:
- Dry run on BII-520: correctly not ready (BII-636/638 allowlist denies Brain code), ~37k tokens, no writes.
- **First live run, BII-811** → draft PR `birunidev/birunidev-space#3` (`autopilot/BII-811` → `bii-504-slice-qa`), 13 unit tests, passed all gates and reviewers in round 1, card moved to Review, ~1 minute.

---

## 3. Environment facts that bite

- **Two opencode versions side by side.**
  - `opencode` = **v2.2.5** (`~/.opencode/bin`). The autopilot uses it via the v2 API: basic auth user `opencode`, password from `OPENCODE_SERVER_PASSWORD`. Sessions are created with `POST /api/session` with a `permissions` ruleset; replies go to `/api/session/{id}/permission/{req}/reply`. `/api/mcp` returns `[]` (useless for health checks).
  - `opencodev1` = **v1.19.5** (`~/.local/opt/opencode-v1`, wrapper sets isolated `XDG_*` dirs and `OPENCODE_CONFIG=~/.config/opencode-v1/opencode.json`). The **TUI office** still speaks the v1 API, so launch it with `tbo-v1 --project <repo>`.
- **Space MCP:** only `https://space.birunidev.com/mcp`. In opencode v2 it is `birunidev-space` in `~/opencode.json` (only loaded for cwd under `$HOME`, which is why worktrees live under `~/.theboringfloor`). The legacy `bii-workspace` entry is disabled. The Go client reuses the OAuth tokens of `birunidev-space` in `~/.local/share/opencode-v1/opencode/mcp-auth.json` and writes rotated tokens back.
- **Observed opencode 2.2.5 permission actions:** `read` (path resource), `glob` (pattern resource like `**/*`), `shell` (not `bash`), `edit`, `birunidev-space_<tool>` with args in `metadata.input`, `opencode_list_mcp_resources`. Unknown actions are rejected; check the run logs when adding new ones.
- **Both target repos' `main` are nearly empty.** The code lives on feature branches; the founder chose bases `bii-504-slice-qa` (space) and `lab/backend-sprint-v2` (lab).
- **uv-managed Python:** the venv must be built from the real interpreter path (`~/.local/share/uv/python/cpython-3.12.16-linux-x86_64-gnu/bin/python3.12`), because the sandbox hides `~/.local/bin` symlinks. The sandbox exposes the uv Python dir and the venv read-only.
- **zsh:** unquoted `$VAR` does not word-split (use `${=VAR}`), and a bare `===` on a line is a zsh error. Many earlier probe failures were this, not the tool.
- `go build ./...` fails on `cmd/kittyprobe` (darwin-only ioctl constants). Pre-existing; build specific packages.
- Test suite: `go vet ./internal/autopilot/... ./cmd/autopilot/ && go test -race -count=1 ./internal/autopilot/...` (9 packages). Live tests only with `AUTOPILOT_LIVE=1` (they spend tokens or read the real board).

---

## 4. Hard rules (enforced in code — keep them that way)

1. **Branch + draft PR only.** Never push to `main` or any base branch, never force-push, never merge, never close PRs, never deploy.
2. **Never move a card to Done.** `board.Guard` plus `SpaceGate` refuse it; config validation refuses routing into Done.
3. **No bypass mode.** Sessions are ask-everything; every permission is decided by `ocv2.Policy` (fail closed). Never send `"always"`. Strip `OPENCODE_CONFIG_CONTENT` from spawned servers.
4. **Checks run sandboxed** (bwrap). No sandbox, no checks.
5. **Secrets:** never print or read whole opencode configs or auth files (they hold tokens). Read only the field you need (e.g. `jq '.mcp.servers["birunidev-space"].url'`). Card comments never carry command output. Don't log token values.
6. **Founder gates:** respect card and board text saying paused / not authorized / design-only / founder approval, plus standing holds (BII-636/638 clean-history allowlist, BII-482/483 security gates, Lab release gate BII-654/658). Only the founder authorizes; never write a FOUNDER AUTHORIZATION header yourself unless the founder explicitly tells you to for a named card.
7. **Git workflow for this repo:** one small PR per change on the fork (`gh pr create --repo birunidev/theboringfloor --base main`), with tests, verified before claiming done. Commit trailer: `Co-Authored-By: <the model you are> <noreply@...>` per your own harness rules. Ask before merging unless the founder says to merge.

---

## 5. Boards (Birunidev Space, workspace `ws_bii`)

| Board | id | Repo / base | Key lists |
|---|---|---|---|
| Birunidev Space — V2 Engineering (Open Source Core) | `board_938353e08e72` | birunidev-space / `bii-504-slice-qa` | Todo `list_e915de2c72f5`, **Autopilot Queue `list_4f9acb16db23`**, Review `list_4e3314cff769`, On Hold `list_a52ce08a1474`, Needs Clarification `list_af39c3c1c5b5`, Done `list_6b61589688b1` |
| Birunidev Lab — Laravel Engineering | `board_e0f865c16b61` | birunidev-lab / `lab/backend-sprint-v2` | Todo `list_6d4fcceac3c3`, **Autopilot Queue `list_3f973c6d08fd`**, In Progress `list_80c30dc3abc8`, Review `list_ea95759d3a8c`, On Hold `list_8d543bc98d34`, Needs Clarification `list_bdbdd3b7668c`, Done `list_74ac63ce6463` |

Cards created for the autopilot:
- **BII-811:** security.py unit tests → in Review, draft PR #3, has a v1 review packet.
- **BII-812:** policy.py unit tests → waiting in Autopilot Queue.
- **BII-813** (V2 Objectives) / **BII-814** (Lab Objectives): review protocol. The founder asked ChatGPT to replace them with `[READ_FIRST — AUTOPILOT v1]` and `[READ_FIRST — CHATGPT v1]` cards on both boards and to audit or standardize all cards. **Board standardization belongs to ChatGPT, not you.** Don't edit board cards unless the founder asks.

---

## 6. What to do next (in order)

1. **Wire READ_FIRST into pre-flight.** When the founder gives you the keys of the new `[READ_FIRST — AUTOPILOT v1]` cards, put them first in each board's `policy_cards` in `config.json`. `Resolve` already appends policy cards to the board policy the pre-flight sees. Then run a `--dry-run --card <some card>` to confirm the pre-flight quotes them.

2. **Run BII-812 live:** `./bin/autopilot --live --once --board "V2 Engineering"`. Verify draft PR, card in Review, review packet, remote `main` unchanged, founder checkout untouched. Note: BII-811 and BII-812 both create `backend/tests/unit/__init__.py` (an identical empty file), so their PRs should not conflict.

3. **PR 8 — TUI cockpit panel (the last planned feature).** Add a read-only panel to the theboringfloor TUI (`internal/panels`, see how `tickets.go` is wired in `internal/app`) that shows:
   - `status.json`: state, card, stage, round, tokens
   - today's tokens vs cap from `usage.json`
   - whether STOP is set
   - the last ~20 events of the current run log (`status.json.logPath`)
   - one key to set or clear the kill switch (call `runstate.Paths.SetStop` / `ClearStop`)
   It must never write anything else. Poll every few seconds. Add a uishot or workspaceshot-style render test. It must work in `tbo-v1`.

4. **Close the loop on ChatGPT reviews.** Verify end to end that a card moved from Review back to Autopilot Queue after a `[CHATGPT REVIEW v1] VERDICT: REQUEST_CHANGES`:
   - resumes the same worktree and branch (`worktree.Prepare` → `Reused`)
   - feeds the findings to pre-flight and implementer (comments are in `CardContext`)
   - pushes new commits to the same branch and reuses the draft PR (`publish.DraftPR`)
   - posts a new packet

   Add an engine test for this with the fakes.

5. **Lab checks in the sandbox.** Lab config links `vendor` from the founder's checkout (setup step) and exposes it read-only. Unverified:
   - `php`, `vendor/bin/pint`, `phpstan` and `php artisan test` running under bwrap (php binary location, extensions, `.env`/`APP_KEY` needs; Laravel's `phpunit.xml` may suffice)
   - whether the `vendor` symlink is git-ignored (Laravel's `/vendor` pattern should match a symlink; confirm, or the scope gate will block and `git add -A` would commit it)

   Test on a detached worktree of `lab/backend-sprint-v2` exactly like the space check verification (see section 3). The Lab board policy still blocks implementation until BII-654/658, so pre-flight should keep bouncing Lab cards.

6. **Frontend checks for space.** No checks for `frontend/` yet (no `node_modules` in worktrees). Option: a shared `node_modules` under `~/.theboringfloor/autopilot/deps/birunidev-space-frontend`, built from the base branch's lockfile, exposed read-only and symlinked by a setup step (git-ignored as `node_modules/`; verify a symlink matches). Then add `npm run lint` and `npm run build` (`tsc -b && vite build`) checks. A build writes `dist/` into the worktree: confirm it is ignored.

7. **Hardening backlog** (each one small PR, with tests):
   - `spaceboard` token refresh can race with opencode v1 writing `mcp-auth.json`: take an advisory lock or re-read and compare before writing.
   - The opencode serve process itself is not sandboxed (only checks are). Consider bwrap around `opencode serve` (needs network for the model API plus read access to its config/auth). At minimum, document the residual risk.
   - Dry-run output shows a blank "would move to" when the board has no working list (cosmetic).
   - A shell request `ls -la a b c` was rejected for metacharacters in BII-811's run: inspect the logged resource and decide whether harmless forms should pass (keep `;&|$<>` and backticks banned).
   - `bin/autopilot --live` as a long-running service (systemd user unit) **only with explicit founder approval**.

8. **Optional, bigger: port the TUI's opencode backend to v2** (`internal/backend/opencode.go` speaks v1). `internal/autopilot/ocv2` is the reference client. Only if the founder asks; `tbo-v1` works today.

---

## 7. How to report back

For each item: what changed (files), the verification commands with their real output, the PR link, and open doubts. Never claim a check passed without showing it.
