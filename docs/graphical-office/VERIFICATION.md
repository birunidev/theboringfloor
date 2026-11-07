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

- The optional structured checks/verdict adapter is absent from the existing
  telemetry. Checks/reviewer pass/fail displays and attributed pass/fail captures
  are therefore not claimed. The specified Unknown fallback is implemented and
  captured. Free-text reviewer JSON is never promoted into a structured verdict.
- There is no board-list feed. Queue is unavailable, not empty; outcome shelves
  are explicitly session records. Configured board names are not inferred card
  attribution.
- Automated browser/keyboard/semantic checks were performed; no manual session
  with a screen reader or physical phone is claimed.
- The graphical PR is independent of the earlier cockpit draft PR. No TUI
  change is included here, and nothing was merged or deployed.
