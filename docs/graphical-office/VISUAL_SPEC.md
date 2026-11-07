# Graphical Office — VISUAL SPEC v1

Implementation contract · 7 November 2026 · Founder-only local application

This document is the visual source of truth. Use restrained Birunidev styling: white surfaces, black typography, cobalt emphasis, flat geometric SVG. No external assets. All dimensions are CSS pixels unless explicitly called SVG units. Do not invent sample cards in the production view.

## 1. Design principles

- Truth before theatre. Animate only confirmed current work; absence of evidence displays “Unknown” or “Unavailable”.
- One worker, one active card. Reviewer desks represent three review roles, not three concurrent coding agents. Exception: with two or more telemetry-confirmed lanes, see Amendment A (one worker per lane).
- Read the room at a glance. Current card, stage, stop state, and budgets take priority over office detail.
- Calm by default. No decorative movement, flashing, confetti, sound, or simulated typing.
- Color reinforces text and icons. Every result has a written label and a distinct symbol.
- One responsive information model. Phone retains every status and control without shrinking desktop text.
- Local and dependable. Embed SVG, CSS, and system fonts; tolerate missing, stale, malformed, or disconnected data visibly.

## 2. Design tokens

### Color

Use semantic CSS custom properties. Hex values below are normative. Text and semantic strokes use the strong foreground tokens; tinted backgrounds never carry meaning alone.

| Token | Light | Dark | Use / contrast requirement |
|---|---|---|---|
| canvas | #F3F5F8 | #0B1020 | Page background |
| surface | #FFFFFF | #111827 | HUD, stations, shelves, dialogs |
| surface-subtle | #F1F5F9 | #1E293B | Floor, inactive props |
| text | #111827 | #F8FAFC | Primary text; >15:1 on surface |
| muted | #475569 | #CBD5E1 | Secondary text; >7:1 on surface |
| border | #64748B | #94A3B8 | Meaningful boundaries; >3:1 on surface |
| divider | #E2E8F0 | #334155 | Decorative separation only |
| accent | #1D4ED8 | #93C5FD | Working, focus, links; >6:1 on surface |
| accent-soft | #EFF6FF | #172554 | Active background; use accent text |
| success | #166534 | #86EFAC | Pass / ready; >6:1 on surface |
| success-soft | #F0FDF4 | #052E16 | Pass background; success text |
| warning | #854D0E | #FDE047 | Near cap / clarification; >6:1 on surface |
| warning-soft | #FEFCE8 | #422006 | Warning background; warning text |
| danger | #B91C1C | #FCA5A5 | Fail / capped / stopped; >5:1 on surface |
| danger-soft | #FEF2F2 | #450A0A | Error background; danger text |
| on-accent | #FFFFFF | #0B1020 | Filled accent controls; >6:1 |
| on-danger | #FFFFFF | #0B1020 | Filled danger controls; >5:1 |

Verify actual combinations during implementation, including tinted surfaces. All normal text ≥4.5:1; large text ≥3:1; interactive boundaries, focus, semantic icons ≥3:1 against adjacent colors. Decorative prop fills may have lower contrast. Never lower opacity on meaningful text, including disabled controls; disable using surface-subtle plus muted and an explicit reason. Focus ring: 3px accent, 3px surface offset. Theme defaults to OS preference; header “Theme” select offers System / Light / Dark, persisted locally.

### Typography

`font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;`

`font-family` for IDs, counts and times: `ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;` Use tabular numerals.

| Role | Desktop | Phone | Weight / line height |
|---|---|---|---|
| App name | 20px | 18px | 650 / 1.3 |
| Current stage | 32px | 26px | 700 / 1.2 |
| Current card key | 24px | 22px | 700 / 1.3 |
| Station / section name | 16px | 16px | 650 / 1.4 |
| Body / title | 16px | 16px | 400 / 1.5 |
| Badge / metadata | 14px | 14px | 600 / 1.4 |
| Timestamp | 12px | 12px | 400 / 1.5 |

Spacing scale: 4, 8, 12, 16, 24, 32, 48. Page gutter 24 desktop, 16 phone. Panel padding 20 desktop, 16 phone. Radius: badges 6, controls 8, panels 12, dialog 16; no pill-shaped panels. Shadows: light `0 2px 8px #1118270A`; dark none. Dialog backdrop `#00000080`. No gradients or textures.

Motion: feedback 120ms; crossfade 160ms; pose frame 240ms; walk frame 150ms; route translation 700ms maximum; easing `cubic-bezier(.2,0,0,1)` for UI, linear for walk. Reduced motion: zero duration for all position/frame effects.

## 3. Layout

The region tables below are the normative wireframes; geometric positioning is specified in section 4. They replace ASCII so the document remains readable with assistive technology.

### Desktop: 1440px viewport

Root max width 1440, centered; 24px side gutters → 1392px content. Vertical document scroll is permitted. Minimum target screenshot height 1000px. Never compress fonts to fit height.

| Region | x | y | Width | Height / structure |
|---|---:|---:|---:|---|
| Header | 24 | 24 | 1392 | 40; name left, theme right |
| HUD | 24 | 80 | 1392 | min 152, grows for note; grid 2fr 1fr 1fr 220px, gap 24 |
| Office floor | 24 | HUD bottom +16 | 1392 | 560; section title then station grid |
| Outcome shelves | 24 | Floor bottom +16 | 1392 | auto min 152; three equal columns, gap 16 |
| Activity ticker | 24 | Shelves bottom +16 | 1392 | auto; latest five events initially |

HUD columns: current card/stage; daily meter; card meter; kill switch. Last-updated status and note are one full-width row below. Office queue desk is on the floor; it is not the outcome region.

### Phone: 390px viewport

16px gutters → 358px content. All regions in one column, 16px vertical gaps. Header height 44. HUD: current card and stage, then two equal meter columns (171px each, gap 16), then full-width kill switch, then note/update status. Floor is an ordered two-column station grid, not a miniaturized desktop drawing. Each cell 171px wide, min 148px tall; gap 16. Station labels remain HTML text. Review desks span the full width with three stacked 88px rows. Queue list spans full width above station grid. Outcome shelves stack full width in the same order as desktop. Ticker follows shelves. No sticky controls, horizontal page scrolling, hover-only information, or automatic scrolling.

Breakpoint: ≥1100 desktop floor; 640–1099 two-column compact floor with HUD two columns; <640 phone rules. Widths use `minmax(0,1fr)`; all text containers `min-width:0`. At 320px and at 200% zoom, collapse compact station grid to one column if cells would be narrower than 144px. Content stays in semantic process order in the DOM regardless of visual layout.

## 4. Office floor

### Desktop grid and route

Inside floor: heading at (20,16), grid area x=24,y=56, width=1344,height=480 at 1440 viewport. Grid has six 224px columns and four 120px rows. Scale column widths with available desktop floor width; row heights remain 120. Coordinates below are relative to grid origin. Station boxes have 16px inset from their allocated cell region.

| Station | Grid placement | Nominal box x,y,w,h | Worker foot anchor |
|---|---|---|---|
| Queue desk | col 1, rows 1–2 | 16,16,192,208 | 112,232 |
| Claim | col 2,row 1 | 240,16,192,88 | 336,112 |
| Setup | col 3,row 1 | 464,16,192,88 | 560,112 |
| Pre-flight | col 4,row 1 | 688,16,192,88 | 784,112 |
| Implement | cols 5–6,row 1 | 912,16,416,88 | 1120,112 |
| Checks | cols 5–6,row 2 | 912,136,416,88 | 1120,232 |
| Acceptance criteria | cols 1–2,row 3 | 16,256,416,88 | 224,352 |
| Tests/lint/build | cols 3–4,row 3 | 464,256,416,88 | 672,352 |
| Diff scope | cols 5–6,row 3 | 912,256,416,88 | 1120,352 |
| Publish | cols 3–4,row 4 | 464,376,416,88 | 672,472 |

Cards and station labels are HTML overlays inside boxes, not scaled SVG text. SVG props occupy a 64×40 icon zone at the left; text starts 76px from box left. Queue contains up to three visible cards, then “Show queue”; if no list feed exists, show “Queue unavailable”. No fabricated count.

Path is a decorative 2px divider stroke in the aisle at y=112,232,352,472. First-run route: queue → claim → setup → pre-flight → implement. Review route: implement → checks only with attributed checks evidence → acceptance → tests/lint/build → diff scope → publish → outcome. A transition follows orthogonal waypoints between foot anchors; use outer aisle x=1336 or inter-row aisle to avoid crossing desk boxes. Reviewer traversal from checks goes left along y=240 then down to acceptance anchor; subsequent reviewers move left to right along y=352. Publish route goes down to y=472 then left. For retry implement:N, move directly along the nearest aisle back to implement. Never animate intermediate stages that were not observed. On initial load, reconnect, or multi-stage jumps, place worker immediately at the confirmed station. Routing is illustration, never elapsed progress.

On phone each compact station shows its prop at top left, label beneath, status below. Only the current station renders the worker at right, foot anchor (cell width−28,84). Transitions crossfade worker out/in in 160ms; there is no walk across the document. Reviewer rows use a right-side 48px character zone. Inactive reviewer characters remain seated.

### Geometric SVG construction

All props use viewBox `0 0 64 40`, 2px border-token stroke, rounded linecaps/joins, surface fill. Desk: top rect (2,20,60,8), rx=3; legs rects (8,28,4,10) and (52,28,4,10). Prop-specific geometry:

| Station | Prop geometry |
|---|---|
| Queue | Three paper rectangles 22×14 at (18,2),(21,5),(24,8), rx=2; two 12px muted strokes per paper |
| Claim | Paper (18,2,22,16), check path M23 10 L27 14 L35 6; neutral until completion |
| Setup | Monitor rect (14,1,36,16), rx=2; stem x32 y17–20; inner bracket paths, decorative only |
| Pre-flight | Open book: paths M32 6 Q20 0 10 5 V17 Q22 12 32 18 and mirrored right half; center line |
| Implement | Monitor as setup; code marks M23 7 L19 10 L23 13 and M41 7 L45 10 L41 13 |
| Checks | Sandbox tray rect (10,7,44,12), rx=2; three outlined squares at x17,28,39,y10,size6; no result implied by prop |
| Review | Clipboard rect (20,2,24,17), rx=2; clip (27,0,10,4); three neutral horizontal lines |
| Publish | Paper (20,4,24,14), two lines; upward arrow M32 12 V0 M27 5 L32 0 L37 5; badge stays unknown until evidence |

Outcome shelf icon: viewBox 64×40, horizontal lines y=18,34,x=4–60; vertical uprights x=4,60,y=4–36; paper outline at (16,4,18,14). The shelf has no paper if no confirmed items. No plants, windows, wall clocks, decorative screens, or extra workers. (Lane workers under Amendment A are not extra workers.) SVG icons `aria-hidden=true`; all information is in adjacent HTML.

## 5. Characters

Flat SVG, not pixel art. Worker is a symbolic faceless operator, 40×48; reviewer 32×40. No gender or ethnicity cues. Worker body accent fill; reviewers muted fill. Heads use surface fill and border stroke. No emotion conveyed solely through faces.

Worker local viewBox 0 0 40 48: head circle (20,9),r=7; torso rounded rect (11,18,18,16),rx=5; arms 3px lines from (12,22) to (6,31) and (28,22) to (34,31); legs from (16,34) to (14,46), (24,34) to (26,46). Card carried at (25,22,12,15),rx=2, surface fill, border stroke, two muted 6px lines. Card exists only with confirmed active card. Foot anchor (20,48). Reviewer uses worker geometry scaled 0.8, seated with legs bent to a chair line, clipboard on lap. Add permanent numbered role marker 1/2/3 in HTML beside each reviewer; names are visible on desks.

Embed one SVG atlas using `<symbol>` for every pose. Do not require a PNG export. Logical atlas specification below also defines a future raster sheet exactly: 320×1152 at 1×, eight 40×48 cells per row, 24 rows. Four character blocks (worker, reviewer1, reviewer2, reviewer3), each six rows in the state order below. Reviewer cells center the 32×40 pose at (4,8). Unused cells transparent; nearest sprite cell must never bleed. SVG implementation references the same character/state/frame naming: `worker-walk-0`, `reviewer2-think-1`, etc.

| State / row within block | Frames | Pose / timing |
|---|---:|---|
| idle / 0 | 1 | Arms down, still; reviewers seated |
| walk / 1 | 4 | Legs ±4px alternate, arms counter-swing ±3px, head fixed; 150ms/frame; worker only in live transit |
| work / 2 | 2 | Arm endpoint shifts 2px toward prop; 240ms/frame, one 480ms cycle when stage first observed, then static frame 0 |
| think / 3 | 2 | Right hand endpoint (26,14) then (27,14); one 480ms cycle, then static frame 0 |
| success / 4 | 2 | Hand raised 4px then relaxed; one 480ms cycle, then idle; check badge persists |
| fail / 5 | 1 | Arms lowered, torso unchanged; stationary; cross badge persists |

No perpetual idle, thinking, working, or success loop. Reviewer walk frames are defined for atlas consistency but never played. Reduced motion always uses frame 0. Worker is not a substitute for an accessible current-stage summary.

## 6. State mapping and evidence rules

### Source contract and precedence

Read exactly the existing `status.json` fields: `state`, `card`, `title`, `stage`, `round`, `tokens`, `note`, `updatedAt`; log `kind` values and given payloads; usage totals/caps; STOP set/unset. New adapters below are optional contracts, not claims that upstream already supplies them.

1. Valid latest status establishes current card and stage. Events enrich results only when attributable to that card and current run. Events never override a newer snapshot with an older stage.
2. STOP is an independent control fact. Set means “Stopped · STOP set”; if status remains running, add “Execution still reported running”. Do not assert current subprocess termination. Unset means “Running · STOP clear” when running, or “Ready · STOP clear” when idle.
3. A successful control response alone does not flip STOP; wait for readback. Cap values do not set STOP automatically. An error event does not imply terminal failure. A rejected permission does not imply the whole card is blocked.
4. Valid usage totals are authoritative for meters. `status.tokens` appears separately as “Reported tokens: {tokens}” if numeric; do not assume it is daily or per-card usage. Missing, negative, NaN or structurally unknown numbers show “Unavailable”. If caps are omitted, use specified defaults 10,000,000/day and 2,000,000/card, labeled “Configured cap”; invalid supplied caps show “Cap unavailable”.
5. `card` is the literal ID/key supplied. Never manufacture a BII key or board name. `title` supplies title. Missing key while running: “Card unavailable”; no carried card graphic. Missing title: “Title unavailable”. `note` is plain text, never executable markup; empty means omit note row. `updatedAt` is last status update, not heartbeat or proof of process life.
6. Keep the latest snapshot on read failure with “Connection lost · Last known state”. Freeze animation. Initial load with no snapshot: “Waiting for status”. Malformed status: “Status unavailable”. No card inference from an arbitrary ticker message.
7. Initialize events from the actual log, do not synthesize start/end on reload. Deduplicate by native event ID or stable file offset. Use log order when timestamps are absent. Do not invent timestamps. New run clears desk results. Without run identity, a confirmed start boundary plus card identity is required to attribute retained results; otherwise show “Unknown”.
8. `updatedAt` older than 60 seconds: “No status update for {age}”; freeze motion but preserve last known labels. This is freshness information, not a failure verdict. Future/invalid timestamp: “Update time unavailable”.

### Minimum optional adapters

Board lists are not in the supplied telemetry. Without an actual board-list adapter, queue is “Queue unavailable”; outcome shelves are explicitly labeled “This session”, populated only from attributable end events. Empty session shelf: “No recorded outcomes”; never claim the board is empty. Optional board adapter must deliver exact board label, list name, card key/title, and retrieval time. Only then render live queue counts and list contents from “Autopilot Queue”, “Review”, “Needs Clarification”, “On Hold”; board labels “V2 Engineering” / “Lab Engineering” require actual source attribution. Proven empty queue shows “Queue empty”; idle plus missing list feed shows “Idle · Queue unavailable”, not “Nothing queued”.

Checks has no matching `stage` enum. `stage-done` does not mean tests passed. Verdict payload is unspecified: engineer must map documented native fields for card/run, target (checks or exact reviewer slug), and result (pass/fail). Never parse free-text note for results. Without this mapping, keep “Checks · Unknown” and reviewer result “Unknown”; ticker can show the literal verdict payload as plain text. Checks worker position requires a confirmed current checks execution signal from such an adapter; result-only evidence updates its badge without moving the worker. No extra fictitious `stage=checks` is written to status.json.

### Primary status and stage table

All neutral badges use muted on surface-subtle; working uses accent on accent-soft; pass uses success on success-soft; fail uses danger on danger-soft. Every badge includes the text below and a matching icon (dash, small solid circle, check, cross, question mark, pause bars). Unknown is never a pass.

| Exact input | Worker position / pose | Station / HUD text and behavior |
|---|---|---|
| `state=idle` | Queue anchor, idle, no carried card | “Idle”; no active desk; card area “No active card”. Confirmed historical outcomes remain. If queue proven empty: “Nothing queued.” |
| `state=running` | Current valid stage anchor | “Running”; card/title visible; stage table below applies |
| `stage=claim` | Claim / work | “Claiming card”; Claim “Working” |
| `stage=setup` | Setup / work | “Setting up”; Setup “Working” |
| `stage=preflight` | Pre-flight / think | “Pre-flight”; desk “Working”; never assume approval |
| `stage=implement:N` | Implement / work | “Implementing”; “Round N of 3” when N in 1..3 |
| `stage=review:acceptance-criteria` | Acceptance / think | “Review · Acceptance criteria”; only reviewer 1 active |
| `stage=review:tests-lint-build` | Tests/lint/build / think | “Review · Tests/lint/build”; only reviewer 2 active |
| `stage=review:diff-scope` | Diff scope / think | “Review · Diff scope”; only reviewer 3 active |
| `stage=publish` | Publish / work | “Publishing draft PR”; no PR link or ready result until confirmed |
| unknown/missing stage while running | Last confirmed anchor or Queue / idle | “Stage unavailable”; display literal unknown stage in details, freeze travel |

`round` supplies HUD round during review/publish and other stages only when valid 1..3. `implement:N` supplies its own N; disagreement with `round` displays “Round data mismatch”, both literal values in details, no guessed round. Missing round → “Round unavailable”. Values outside 1..3 → “Round out of range”; never clamp a label. Keep prior station completion as “Done” only when attributable stage-done exists. A completed reviewer with no explicit verdict is “Done · Verdict unknown”. Idle snapshots take precedence over retained nonempty stage/card fields for the active view.

### Event table

| `kind` / payload | Visual mapping |
|---|---|
| `start` | Ticker “Run started” with actual card if given; clear attributable prior-run badges; do not advance worker without stage evidence |
| `stage` | Ticker “Stage: {native stage}”; may establish station only with validated current-run stage payload when no newer snapshot contradicts it |
| `stage-done` | Ticker “Stage done: {native stage}”; attributed station “Done”, neutral check outline; not “Pass” |
| `permission {action,allow:true}` | Ticker success check “Allowed: {action}”; no desk or worker movement |
| `permission {action,allow:false}` | Ticker danger cross “Rejected: {action}”; no invented block/stop |
| `permission` missing/invalid allow | “Permission result unavailable: {action}”; unknown badge |
| `tokens` | Ticker “Token usage updated”; update meters only if adapter documents scope and supplies valid totals; otherwise usage feed remains authoritative |
| `verdict` attributed pass | Target desk “Pass”, success; matching reviewer success once; worker stays at current confirmed stage |
| `verdict` attributed fail | Target desk “Fail”, danger; reviewer fail; do not invent retry or outcome |
| `verdict` unattributed/unknown result | “Verdict received · Details unavailable” plus expandable native payload; no desk result |
| `error` | Ticker danger “Error: {native message}”; HUD “Error reported”, details visible; freeze travel until next confirmed stage/update; current stage preserved |
| `end {kind:ready-for-review}` | Session Review shelf gets actual card; “Draft PR ready”; success; worker rests at shelf marker only if this terminal event is latest current-run evidence |
| `end {kind:needs-clarification}` | Session Needs Clarification shelf; “Questions asked”; warning; no invented question text |
| `end {kind:blocked}` | Session On Hold shelf; “Blocked”; warning; reason only if provided |
| unknown event or unknown end kind | Ticker “Unrecognized event”; native payload available; never infer an outcome |

End events set terminal session display only until a newer active status/run. Repeated end events do not duplicate cards for the same run. Session outcomes are run records, not authoritative board membership; a later run may produce another record. PR URL appears only if explicitly supplied and validated as http(s); link label “Open draft PR”. Never synthesize a URL from card ID. Terminal events do not set STOP.

### Overlays and budget states

| Condition | Display / animation |
|---|---|
| STOP set | Danger “Stopped · STOP set”; worker static at last confirmed anchor; active desk “Last reported: {stage}”; Resume control |
| STOP unset | Running/Ready labels above; Stop control; normal evidence-driven movement |
| STOP unreadable | “Stop state unavailable”; controls disabled with reason; no assertion of running/stopped |
| usage ratio <0.8 | Accent meter, “Within cap” |
| usage ratio ≥0.8 and <1 | Warning meter, “Near cap”; same worker behavior |
| usage ratio ≥1 | Danger meter, “Cap reached”; actual numerator may exceed cap; fill capped at 100%; no fake termination |
| any cap reached while running | Add “Execution still reported running”; no automatic stop/resume |
| missing usage/cap | Unfilled meter, “Usage unavailable” / “Cap unavailable”; never show zero |
| connecting / data error | “Waiting for status” / “Connection lost”; freeze; retain last known labels when available |

Visual precedence: connection/invalid-state banner first; STOP banner second; error and budget warnings next; current/last-known stage always visible. Stop, cap, error, terminal outcome, and freshness are separate facts, not one mutually exclusive state. New valid data clears only its corresponding data-error condition.

## 7. Components

### Card chip

Surface, 1px border, radius 8, padding 12. Key 14px mono/700; title 16px normal beneath; optional actual source board in 14px muted. Queue/shelf title clamps at two lines; key never truncates. HUD title clamps at two lines desktop, three phone. Every truncated item has a keyboard/touch “Show full title” disclosure expanding inline; do not depend on `title` attribute. Break unbroken words with `overflow-wrap:anywhere`. Empty/missing title reads “Title unavailable”. Chips without an actual URL are not fake links. Active card uses 2px accent border and “Active” badge. No avatars, estimates, progress percentages, or fake IDs.

### Desk badge

Minimum 24px height, 6px radius, 6px horizontal padding, 14px semibold. Icon 14px + 6px gap. States: “Unknown” (?), “Waiting” (dash; only when current-run sequence is known), “Working” (circle), “Done” (outline check), “Pass” (check), “Fail” (cross). Long variants wrap; never ellipsize result text. Initially all desks “Unknown”; do not label unobserved work “Waiting” when execution plan is unknown. Active reviewer has accent border around desk; seated reviewers do not pretend to work simultaneously.

### HUD meters

Two labeled `<meter>` elements with visible text equivalents; 8px-high tracks, surface-subtle fill, 1px border. Accessible minimum 0, maximum valid cap, value min(actual,cap); accompanying text contains full actual value. Format headline “1.6M / 10M” with ≤2 decimals, exact integers in disclosure, percentage rounded to nearest integer; thresholds use unrounded ratio. Example numbers are documentation only, never placeholders in live UI. Labels “Today” / “This card”. Cap source shown in details. Daily reset/timezone is owned by usage feed; UI never computes a new day from browser timezone. Card usage must match current card or show “Usage unavailable”.

### Kill switch and confirmation

Minimum button height 44; width 100% in control column; 16px semibold. STOP clear → filled danger “Stop autopilot”. STOP set → outlined accent “Resume autopilot”. State unknown → disabled “Control unavailable”. Confirm both operations using accessible modal, max width 400, viewport gutters 16, padding 24; heading 20px; buttons stacked on phone. Focus starts on Cancel, trapped inside; Escape cancels; return focus to trigger.

Stop dialog: heading “Stop autopilot?”; body “Set STOP. Work may continue until the autopilot checks it.”; buttons “Cancel”, “Set STOP”. Resume dialog: heading “Resume autopilot?”; body “Clear STOP. The autopilot may continue work.”; buttons “Cancel”, “Clear STOP”. These statements intentionally make no claim of immediate interruption. Scope note: “This control changes STOP only.” Do not add budgets or policy overrides.

After confirm: “Setting STOP…” / “Clearing STOP…”, disable duplicate submission, retain old state. Readback success → factual state label; 10-second unconfirmed readback → “Change not confirmed. Check STOP state.” Control error → “Could not change STOP. Try again.” Retry requires a new confirmation. Never optimistically animate resumption. Backend mutation must be local and authenticated as appropriate; this spec defines appearance, not a new backend endpoint.

### Outcome shelves

Desktop three equal panels, phone stacked: “Review”, “Needs Clarification”, “On Hold”. Subtitle “This session” unless actual board feed exists, then “Board lists”. Show latest three confirmed items per shelf, “Show all ({actual count})” expands inline. No count when feed unavailable. Empty session “No recorded outcomes”; proven empty live list “No cards”; failed live list “List unavailable”, retain last snapshot with freshness label. Outcome colors: success, warning, warning respectively. Draft-ready is not merged, deployed, or approved.

### Activity ticker

A stationary event list, not a marquee. Latest first; five rows, “Show older events” reveals 20 more at a time from actual log. Each row: 16px icon, 14px message, optional 12px native event timestamp. If no timestamp show no clock value. At phone widths timestamp moves beneath message. Action names and errors wrap to two lines; “Details” expands complete text/native payload in plain-text `<pre>` with wrapping. Empty confirmed log: “No events recorded.” Unavailable log: “Activity unavailable”. Never substitute “No events” for failed loading.

Use a separate visually hidden polite live region for new stage, verdict and outcome summaries; batch event announcements to at most once every 2 seconds. Do not announce token changes or entire log history on load. Control failure uses `role=alert`; repeating background errors are deduplicated. Regions have headings; main status summary is readable without SVG.

## 8. Motion

- A newly confirmed adjacent stage moves worker along the shortest valid aisle, 700ms total regardless of distance; four-frame walk only during travel. Snapshot updates arriving mid-transition cancel travel and place at newest anchor; never queue theatrical catch-up.
- Arrival gives one work/think cycle, then still pose. A subsequent explicit stage event may trigger one new cycle. Polling identical state does not restart it.
- New confirmed pass gives one success cycle; fail is static. Badge changes crossfade 120ms. New event row fades in 160ms, no slide.
- Meter width updates in 160ms only on new usage. No pulsing fills. No spinner that implies coding; loading displays static text “Loading…”.
- Idle, STOP set, stale data, disconnection, unknown stage, background document: no movement. Returning to foreground places current worker immediately.
- `prefers-reduced-motion:reduce` disables all transitions, frame cycling and smooth scrolling. Current worker relocates instantly and active outline/text remain. A header “Motion” select Auto / Off is persisted; Off overrides OS. No mode may override OS reduced motion to enable movement.

## 9. Exact copy

Use sentence case except the source list name “Needs Clarification” and STOP. Preserve source card titles verbatim, escaped.

| Location | Copy |
|---|---|
| Header | “Graphical Office” |
| Subtitle | “Coding autopilot” |
| Floor heading | “Office” |
| Queue heading | “Autopilot Queue” |
| Process labels | “Claim”, “Setup”, “Pre-flight”, “Implement”, “Checks”, “Acceptance criteria”, “Tests/lint/build”, “Diff scope”, “Publish” |
| HUD headings | “Current card”, “Today”, “This card”, “Kill switch” |
| Round | “Round {n} of 3” |
| Update line | “Status updated {relative age}” with exact supplied time in disclosure |
| No active work | “No active card” |
| Confirmed idle + empty queue | “Nothing queued.” |
| Active but unknown queue | “Queue unavailable” |
| No checks mapping | “Checks · Unknown” |
| No review result | “Verdict unknown” |
| Outcome section | “Outcomes” |
| Ticker heading | “Activity” |
| General fallback | “Unavailable” / “Unknown” as defined above |

All state-specific text and dialog text in sections 6–7 are also normative. Dynamic values remain literal source data; never invent completion time, remaining time, sandbox name, branch, PR, error explanation, question, board attribution, or reviewer verdict.

## 10. Acceptance checklist

### Visual and responsive

- [ ] Capture 1440×1000 and 390×844 in light and dark (four full-page screenshots), plus reduced-motion captures at both widths and both themes (eight total). Use explicit synthetic fixtures only in test mode, visibly marked “TEST DATA”; production has no fixture fallback.
- [ ] For each width/theme verify idle, implement round 2, all three individual review stages, attributed checks pass/fail, three terminal outcomes, unknown checks/verdict, STOP while status still running, near-cap/capped, missing data, error, stale data, long title. These are additional state captures beyond the eight baseline captures.
- [ ] Desk placement, typography, semantic colors, grid gaps and control sizes match this spec. No overflow at 390 or 320px; 200% zoom retains all content and controls.
- [ ] Reviewer desks are side by side on desktop and vertically ordered on phone. Current card/stage and STOP state remain the strongest hierarchy.

### Evidence fidelity

- [ ] Every enumerated state/stage/event maps as specified; unknown enum values fail visibly without crashing.
- [ ] No queue feed never renders zero queued; idle alone never claims empty queue. No event log never renders empty history.
- [ ] Stage completion never becomes pass. Verdict without documented target never colors a reviewer. No checks event never creates a fictitious checks stage.
- [ ] STOP set with running telemetry displays both facts; capped budget never claims a stop. Permission rejection and error never invent a terminal outcome.
- [ ] Reload, duplicate log rows, old-card events, stale snapshots, quick stage changes and a new run cannot replay animations or leak previous verdicts into the new run.
- [ ] Exact usage totals, unknown values, ≥80% and ≥100% thresholds, over-cap numerator, conflicting round fields and missing cap behavior are verified.

### Accessibility and local delivery

- [ ] Keyboard reaches theme, motion, disclosures, event history and stop/resume; focus is visible. Dialog traps/returns focus and Escape cancels.
- [ ] Normal text contrast ≥4.5:1; large text and meaningful UI ≥3:1, measured in both themes for every actual foreground/background pair including tinted panels.
- [ ] Screen reader exposes current card, stage, round, both usage totals/caps, STOP state, verdict labels and events without reading decorative SVG.
- [ ] Reduced motion has no translation, pose cycling, fade, smooth scroll or meter transition; labels preserve all information.
- [ ] Repeated polling does not create repeated live announcements; permission action names and full long titles are accessible by keyboard and touch.
- [ ] All buttons/touch disclosures are at least 44×44px hit areas; text remains selectable.
- [ ] Browser network inspection shows only loopback-origin application/data/control requests; no fonts, CDNs, remote images, analytics or external asset loads. Actual PR links open only on deliberate activation, with `rel=noopener` when opening a new tab.
- [ ] Offline/failed local reads preserve a clearly marked last-known view. A failed control write never flips the STOP indicator.

Build deliverable: one local page, embedded SVG symbol atlas and CSS tokens, semantic HTML status/components, documented telemetry adapter mappings, and the screenshot evidence above. No generated mockups are required: the coordinate and state contracts are authoritative.

## Amendment A — Parallel lanes (CTO decision, 8 November 2026)

Evolves spec v1 for runs with parallel lanes. It does not change v1 behavior for zero or one lane, and it is not approval to merge or to start workers. Telemetry rules live in `TELEMETRY.md` ("Parallel lanes").

1. **Source.** A lane worker is drawn only for a lane in `workers.json` that passes the pairing rule: `workers.json.updatedAt` equals `status.json.updatedAt`. Unpaired, stale, malformed or missing data draws no lane workers and shows the stated reason.
2. **One worker per lane.** With two or more usable lanes, draw one worker per lane, using the v1 worker geometry (section 5) at that lane's last reported desk. With one lane, or none, the v1 rule (one worker, one active card) applies unchanged.
3. **No theatre.** No simulated workers, invented lanes, fictional work animation, or walking between desks in lane mode. A lane worker is static at its last reported desk. Checks and guardrail results stay "Unknown" unless attributed telemetry exists; a lane is never shown as PASS without it.
4. **Shared desk.** When two lanes report the same desk, both stay identifiable: each worker carries its lane name as visible HTML text, offset so neither hides the other. Identity is never conveyed by color alone and never relies on `title` or hover.
5. **Accessibility.** Each lane's card, stage and round are in the text lane list, not only in SVG. Lane workers are `aria-hidden`; the lane list is the accessible equivalent.
6. **Precedence.** Where this amendment conflicts with v1 wording about extra or concurrent workers (section 1 and section 4), this amendment wins for two or more lanes only.

## Amendment B — Explain questions rather than generic outcomes (8 November 2026)

Founder decision: when pre-flight moves a card to Needs Clarification, its
outcome card must disclose the **actual pre-flight context**, not only “card is
not ready to implement; no code was changed”.

1. Show the pre-flight summary as “Why clarification is needed”; show any
   specific quoted policy/source blocker and **numbered founder questions**.
2. Cite the exact card key and run ID in the text, and label the material
   “Pre-flight reported · not independently verified”. These are session
   records, not a live Space board-list feed or an independent legal ruling.
3. Render log fields as escaped text. Do not insert arbitrary HTML, expose
   auth/config secrets, or display the full ChatGPT prompt. The telemetry
   schema limits lengths and count; the client independently bounds them.
4. If the pre-flight context was not recorded (old runs, invalid logs), show
   “Clarification details were not recorded for this run” and direct the
   founder to the existing card comment. Do not manufacture explanation text
   from the generic outcome, infer missing questions or enable a retry button.
5. Preserve visual tokens, 44px control targets, responsive 390px/1440px,
   read-only/STOP semantics, daily token and per-card caps, and source
   precedence from the original v1 spec and Amendment A.

This amendment changes **explanation only**. It does not authorize dispatch,
merge, deployment, closing questions, or new source access.
