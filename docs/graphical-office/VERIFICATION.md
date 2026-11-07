# Graphical Office verification

7 November 2026. CODEX.md section 6, item 8. Fork-only draft PR; no merge or deployment.

## What changed

- `cmd/officeview/main.go`: loopback-only listener, CLI and shutdown.
- `cmd/officeview/state.go`: rooted, bounded telemetry merge and native log records.
- `cmd/officeview/server.go`: embedded assets, state/SSE endpoints, opt-in token/Origin-protected STOP controls.
- `cmd/officeview/assets/{index.html,style.css,app.js}`: semantic page, normative tokens/layout/geometry, embedded SVG symbol atlas, source-driven finite motion, theme/motion preferences and accessible confirmations.
- `cmd/officeview/server_test.go`: temporary-home handler/security/SSE tests.
- `cmd/officeview/{browser-proof,contrast-proof}.mjs`: reproducible real-server browser and contrast checks; no production fixture fallback.
- `README.md`, `TELEMETRY.md`, the verbatim supplied `VISUAL_SPEC.md`, and this evidence directory.

No autopilot engine, permission policy, board routing or target repository code changes.

## Go checks

Built with `go build -o /tmp/officeview-browser/officeview ./cmd/officeview` (exit 0, no output).
Final handler checks and all nine autopilot packages ran in bubblewrap:

```sh
bwrap --die-with-parent --new-session --unshare-net --unshare-pid --unshare-ipc \
  --ro-bind / / --dev /dev --proc /proc --tmpfs /tmp --tmpfs /home/birunidev \
  --ro-bind /home/birunidev/Documents/projects/theboringfloor /home/birunidev/Documents/projects/theboringfloor \
  --ro-bind /home/birunidev/go/pkg/mod /home/birunidev/go/pkg/mod \
  --chdir /home/birunidev/Documents/projects/theboringfloor \
  --setenv HOME /home/birunidev --setenv GOCACHE /tmp/go-cache \
  --setenv GOMODCACHE /home/birunidev/go/pkg/mod --setenv GOPROXY off \
  -- /usr/bin/sh -c 'go vet ./cmd/officeview && go test -race -count=1 ./cmd/officeview ./internal/autopilot/...'
```

Real output (vet exited 0 without output):

```text
ok github.com/theboringhumane/theboringfloor/cmd/officeview 1.101s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/board 1.010s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/engine 1.418s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/handoff 1.012s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/ocv2 1.092s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline 1.019s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/publish 1.277s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/runstate 1.022s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard 1.044s
ok github.com/theboringhumane/theboringfloor/internal/autopilot/worktree 1.305s
```

The handler tests cover state merging, server-local day rollover, malformed and
unknown numeric fields, specified cap defaults, idle retained-card handling,
missing/corrupt files, per-process token uniqueness, missing/wrong Origin/token,
read-only mode, STOP symlinks, DNS-rebinding Host checks, rejected bind addresses,
traversal/escaping log symlinks, embedded assets/CSP, no GET writes, SSE append,
partial lines, file switch and same-path inode rotation.

## Browser checks and screenshots

```sh
npm install --prefix /tmp/officeview-browser playwright
go build -o /tmp/officeview-browser/officeview ./cmd/officeview
PLAYWRIGHT_MODULE=/tmp/officeview-browser/node_modules/playwright/index.mjs \
  node cmd/officeview/browser-proof.mjs
```

Chrome executable: `/usr/bin/google-chrome`. The script starts the real Go
server against a temporary home with `--controls --test-data`, writes actual
fixture files, inspects browser requests, exercises controls and stops its
server afterward. Every image visibly says **TEST DATA**. Full raw browser
output is in [browser-output.txt](evidence/browser-output.txt).

Real output:

```text
1440/light/auto: layout, labels, controls, network, screenshot PASS
1440/light/reduced: layout, labels, controls, network, screenshot PASS
1440/dark/auto: layout, labels, controls, network, screenshot PASS
1440/dark/reduced: layout, labels, controls, network, screenshot PASS
390/light/auto: layout, labels, controls, network, screenshot PASS
390/light/reduced: layout, labels, controls, network, screenshot PASS
390/dark/auto: layout, labels, controls, network, screenshot PASS
390/dark/reduced: layout, labels, controls, network, screenshot PASS
1440/light: 19 evidence-state captures PASS
1440/dark: 19 evidence-state captures PASS
390/light: 19 evidence-state captures PASS
390/dark: 19 evidence-state captures PASS
Controls: cancel focus, focus trap, Escape, STOP set/readback, resume/readback PASS
Disclosures/history, persisted preferences, 320/640/900/1099px, 200% zoom, escaped XSS, last-known disconnected state PASS
Real-file stage change: 700ms aisle travel, finite arrival pose, no replay on load/identical polls, idle static, rejection not terminal PASS
Reduced-motion live stage change uses frame 0; failed STOP write keeps previous state PASS
New confirmed snapshot clears the earlier error condition while preserving the reported stage PASS
```

Eight baseline full-page captures use 1440×1000 and 390×844 viewports:

| Viewport | Light | Dark | Light, reduced | Dark, reduced |
|---|---|---|---|---|
| 1440×1000 | [Image](evidence/baseline-1440-light.png) | [Image](evidence/baseline-1440-dark.png) | [Image](evidence/baseline-1440-light-reduced.png) | [Image](evidence/baseline-1440-dark-reduced.png) |
| 390×844 | [Image](evidence/baseline-390-light.png) | [Image](evidence/baseline-390-dark.png) | [Image](evidence/baseline-390-light-reduced.png) | [Image](evidence/baseline-390-dark-reduced.png) |

There are 76 additional state captures, named `state-<case>-<width>-<theme>.png`:
idle; implement round 2; each of the three individual review stages; unknown
checks/verdict; each terminal outcome; STOP while running; near cap; over cap;
missing data; error; stale status; long title; conflicting rounds; unknown
prototype-like stage name; invalid cap. Each runs at both widths in both themes.

Baseline checks assert the desktop floor is 1392×560 at x=24 and a normal desk
is 88px tall; all visible buttons, selects and disclosures have at least 44px
hit areas. Browser requests are checked against the exact loopback origin.
Reduced motion is checked for zero active animations. Additional viewport and
zoom checks detect horizontal overflow. Long titles and attempted HTML markup
are displayed as text with full-title disclosures.

## Contrast and audit

```sh
OFFICEVIEW_CONTRAST_OUTPUT=docs/graphical-office/evidence/contrast-output.txt \
  node cmd/officeview/contrast-proof.mjs
git diff --check
```

Contrast checks pass for 50 foreground/background combinations, including
tinted semantic panels, surface text, disabled text, controls and focus/strokes.
Examples from the real output:

```text
light danger on danger-soft: 5.91:1 PASS (>=4.5)
light accent on accent-soft: 6.16:1 PASS (>=4.5)
light border on surface-subtle: 4.34:1 PASS (>=3)
dark warning on warning-soft: 11.06:1 PASS (>=4.5)
dark border on surface-subtle: 5.71:1 PASS (>=3)
```

Full ratios are in [contrast-output.txt](evidence/contrast-output.txt). The
after-implementation visual audit and delivery gate are recorded separately.
`git diff --check` exited 0 without output. A byte comparison confirmed that
VISUAL_SPEC.md matches the supplied attachment.

## Open doubts and limits

- Structured checks/gate telemetry is absent. Sandboxed checks and the scope,
  sensitive/CI and diff-evidence guardrails therefore remain Unknown. Legacy
  reviewer log records are retained only as literal Activity history and never
  activate office workers.
- There is no board-list feed. Queue is unavailable, not empty; outcome shelves
  are explicitly session records. Configured board names are not inferred card
  attribution.
- Automated browser/keyboard/semantic checks were performed; no manual session
  with a screen reader or physical phone is claimed.
- The graphical PR is independent of the earlier cockpit draft PR. No TUI
  change is included here, and nothing was merged or deployed.

## Task launcher follow-up

Added root `Taskfile.yml`, `scripts/run-autopilot-office.sh`, its isolated stub
test, README commands and the `.task/` ignore rule. `task all` builds the UI and
worker, preserves the existing cockpit binary if its source is on the other
draft branch, then runs the cockpit in the foreground with UI/worker logs in a
private temporary directory. Exiting the cockpit terminates only this launcher's
background processes. No command automatically clears STOP.

Verification:

```sh
task --list
task --dry all
task --dry once BOARD='Lab Engineering'
bash -n scripts/run-autopilot-office.sh
task build
task status
bwrap --die-with-parent --unshare-net --ro-bind / / --dev /dev --tmpfs /tmp -- \
  /usr/bin/bash /home/birunidev/Documents/projects/theboringfloor/scripts/test-autopilot-office.sh \
  /home/birunidev/Documents/projects/theboringfloor/scripts/run-autopilot-office.sh
git diff --check
```

Real output excerpts:

```text
Preserving the existing TUI binary: cockpit source is on codex/autopilot-cockpit.
state=idle card= stage= tokens=0 stopped=false updated=2026-11-07T00:15:18Z last: BII-812 ready-for-review
PASS: board/project quoting, cockpit exit code, UI/worker cleanup, STOP preserved; no live worker used
```

Task listed all commands and both dry runs compiled successfully. Builds exited
0; syntax/diff checks exited 0 without output. The first stub test invocation
lacked bubblewrap's private `/dev` mount and failed to access `/dev/null`; the
corrected sandbox command above passed. No live board worker was started during
this verification. To run actual work, the founder invokes `task all`,
`task worker` or `task once` explicitly.

## Office readability additions — verification (7 Nov 2026)

Changed: `cmd/officeview/{state.go,server_test.go,assets/app.js,assets/index.html,assets/style.css}`,
new `cmd/officeview/office-proof.mjs`, `TELEMETRY.md` (new section), evidence `office-*.png` and
`office-proof-output.txt`.

```sh
go vet ./cmd/officeview && go test -race -count=1 ./cmd/officeview ./internal/autopilot/...
go build -o /tmp/officeview-browser/officeview ./cmd/officeview
PLAYWRIGHT_MODULE=/tmp/officeview-browser/node_modules/playwright/index.mjs \
OFFICEVIEW=/tmp/officeview-browser/officeview OUT=docs/graphical-office/evidence \
  node cmd/officeview/office-proof.mjs        # 212 checks, 28 screenshots
OFFICEVIEW_SHOTS=/tmp/codex-shots PLAYWRIGHT_MODULE=... node cmd/officeview/browser-proof.mjs   # existing proof, PASS
PLAYWRIGHT_MODULE=... node cmd/officeview/contrast-proof.mjs                                   # PASS
```

All fixtures are TEST DATA homes (idle with history, active implement, between-stage
guardrails, blocked, stopped), rendered with `--test-data` so the page shows the
TEST DATA banner.

## Coding-only office correction (7 Nov 2026)

The founder moved review outside the autopilot. This correction removes the
three internal reviewer characters and their parallel-work inference. Their
floor row now contains passive deterministic guardrails: Scope gate,
Sensitive/CI gate and Diff evidence gate. Sandboxed checks and all three gates
remain visibly Unknown until the engine emits structured live telemetry. Old
`review:*` log rows remain literal Activity history and do not activate a desk.

Design Read: local operational monitor for the founder, using the existing
restrained geometric office language, dial ENERGY 1 / RHYTHM 1 / MOTION 1.

- Color stays on the normative semantic tokens because state, focus and errors
  must remain distinguishable in both themes.
- The process layout stays linear because it mirrors the coding sprint and
  keeps the active card as the sole focal point.
- System typography stays because local reliability and readability take
  priority over brand display type.
- Existing spacing stays because it preserves 44px controls and the 390px
  no-overflow contract.
- Guardrails use desks because they are real pipeline steps; no character is
  attached because the engine, rather than another agent, performs them.
- The clipboard becomes a gate checklist because the same compact geometry now
  denotes deterministic evidence without implying an external reviewer.

Verification:

```sh
bwrap --die-with-parent --unshare-net ... -- \
  /usr/bin/sh -c 'go vet ./cmd/officeview ./internal/autopilot/... && \
  go test -race -count=1 ./cmd/officeview ./internal/autopilot/...'
PLAYWRIGHT_MODULE=/tmp/officeview-browser/node_modules/playwright/index.mjs \
OFFICEVIEW=/tmp/officeview-browser/officeview \
OUT=docs/graphical-office/evidence node cmd/officeview/office-proof.mjs
PLAYWRIGHT_MODULE=/tmp/officeview-browser/node_modules/playwright/index.mjs \
OFFICEVIEW=/tmp/officeview-browser/officeview \
OFFICEVIEW_SHOTS=/tmp/office-coding-only-baseline node cmd/officeview/browser-proof.mjs
PLAYWRIGHT_MODULE=/tmp/officeview-browser/node_modules/playwright/index.mjs \
node cmd/officeview/contrast-proof.mjs
node --check cmd/officeview/assets/app.js
node --check cmd/officeview/office-proof.mjs
node --check cmd/officeview/browser-proof.mjs
git diff --check
```

Real output: vet was silent; all 10 race-tested packages passed. Office proof
wrote 28 TEST DATA screenshots and ended `ALL CHECKS PASSED`. Browser proof
passed 16 evidence states at 1440px and 390px in light/dark mode, all controls,
history, 320/640/900/1099px layouts, 200% zoom, XSS escaping, disconnection,
motion and reduced-motion checks. Contrast proof and all three syntax checks
exited 0 without output. `git diff --check` exited 0 without output.

Antislop delivery gate: PASS. No new gradient, glow, glass, decorative motion,
fabricated activity, dead control, external asset or unsupported claim was
introduced. The real-browser proof covers overflow, keyboard dialog behavior,
focus return, both themes, reduced motion, local-only requests, empty/idle,
active, blocked, stopped and unavailable telemetry states. One active card is
the focal point; cobalt remains the single deliberate activity/focus accent;
flat geometric station props remain the identity motif.

Open limit: the board has separate Frontend In Progress and Backend In Progress
lanes, but the current runtime exposes one `status.json` and executes one card at
a time. This PR does not simulate lane concurrency. Parallel frontend/backend
execution requires a separate engine/status contract and must preserve per-card
worktrees, the shared token budget, STOP and claim safety.
