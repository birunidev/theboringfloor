// office-proof.mjs — real-browser proof for the office improvements.
//
// Builds TEST DATA autopilot homes (idle with history, active implement,
// between-stage guardrails, blocked, stopped), runs the real officeview binary on
// each with --test-data, asserts truthfulness in the DOM, checks that every
// request stays on loopback, and captures screenshots.
//
//   go build -o /tmp/officeview-browser/officeview ./cmd/officeview
//   PLAYWRIGHT_MODULE=/tmp/officeview-browser/node_modules/playwright/index.mjs \
//   OFFICEVIEW=/tmp/officeview-browser/officeview \
//   OUT=docs/graphical-office/evidence node cmd/officeview/office-proof.mjs
import { spawn } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync, utimesSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const BIN = process.env.OFFICEVIEW || "/tmp/officeview-browser/officeview";
const OUT = process.env.OUT || "docs/graphical-office/evidence";
mkdirSync(OUT, { recursive: true });

const iso = (offsetSeconds = 0) => new Date(Date.now() + offsetSeconds * 1000).toISOString();
const put = (home, path, value) => {
  mkdirSync(join(home, path, ".."), { recursive: true });
  writeFileSync(join(home, path), JSON.stringify(value) + "\n", { mode: 0o600 });
};
const runLog = (home, card, run, records, modifiedAgoSeconds = 0) => {
  const dir = join(home, "runs", card);
  mkdirSync(dir, { recursive: true });
  const path = join(dir, `${run}.jsonl`);
  writeFileSync(path, records.map((r) => JSON.stringify({ card, run, ...r })).join("\n") + "\n", { mode: 0o600 });
  const t = new Date(Date.now() - modifiedAgoSeconds * 1000);
  utimesSync(path, t, t);
  return path;
};
const perm = (stage, action, allow, msg, ts) => ({ kind: "permission", stage, ts, msg, data: { action, allow, resources: ["x"] } });

// Each fixture writes a complete TEST DATA home and returns DOM expectations.
const fixtures = {
  idle(home) {
    put(home, "usage.json", { day: iso().slice(0, 10), today: 2320000, cards: {} });
    runLog(home, "BII-TEST-READY", "r-ready", [
      { kind: "start", ts: iso(-3600), msg: "claimed (TEST DATA)" },
      { kind: "end", ts: iso(-3500), msg: "passed", data: { kind: "ready-for-review", summary: "passed sandboxed checks and gates in round 1; awaiting external review", pr: "https://github.com/birunidev/birunidev-space/pull/3", rounds: 1 } },
    ], 3500);
    runLog(home, "BII-TEST-CLARIFY", "r-clarify", [
      { kind: "start", ts: iso(-1800), msg: "claimed (TEST DATA)" },
      { kind: "stage", stage: "preflight", ts: iso(-1790), msg: "session ses_TEST_preflight" },
      perm("preflight", "read", true, "read inside worktree", iso(-1785)),
      { kind: "stage-done", stage: "preflight", ts: iso(-1700), msg: '{"ready": false}', data: { billable: 37206 } },
      { kind: "end", ts: iso(-1690), msg: "not ready", data: {
        kind: "needs-clarification",
        summary: "card is not ready to implement; no code was changed",
        rounds: 0,
        clarification: {
          summary: "Missing approval for clean-room frontend imports",
          policyBlock: "BII-638 source allowlist still needs per-file review",
          questions: ["May app.css be written from scratch?", "Who owns main.tsx? <script>TEST DATA</script>"],
        },
      } },
    ], 1690);
    runLog(home, "BII-TEST-LEGACY", "r-legacy", [
      { kind: "end", ts: iso(-2400), msg: "not ready", data: { kind: "needs-clarification", summary: "card is not ready to implement; no code was changed" } },
    ], 2400);
    put(home, "status.json", { state: "idle", updatedAt: iso(-5), note: "last: BII-TEST-CLARIFY needs-clarification (TEST DATA)" });
    return {
      strip: "Idle · last run BII-TEST-CLARIFY: Questions asked",
      stripContext: "Missing approval for clean-room frontend imports",
      activity: "Last run · BII-TEST-CLARIFY",
      shelvesSubtitle: "Today, from run logs",
      reviewShelfCard: "BII-TEST-READY",
      clarification: {
        card: "BII-TEST-CLARIFY",
        texts: ["Missing approval for clean-room frontend imports", "BII-638 source allowlist", "May app.css be written from scratch?", "Who owns main.tsx?", "r-clarify"],
        questions: 2,
        escapedHTML: "<script>TEST DATA</script>",
      },
      legacyClarification: "BII-TEST-LEGACY",
      workerTagHidden: true,
      activeStations: 0,
    };
  },
  active(home) {
    put(home, "usage.json", { day: iso().slice(0, 10), today: 1600000, cards: { "BII-TEST-ACTIVE": 41000 } });
    const log = runLog(home, "BII-TEST-ACTIVE", "r-active", [
      { kind: "start", ts: iso(-60), msg: "claimed (TEST DATA)" },
      { kind: "stage", stage: "preflight", ts: iso(-55), msg: "session ses_TEST_preflight" },
      perm("preflight", "read", true, "read inside worktree", iso(-54)),
      { kind: "stage-done", stage: "preflight", ts: iso(-45), msg: '{"ready": true}', data: { billable: 18820 } },
      { kind: "stage", stage: "implement:1", ts: iso(-44), msg: "session ses_TEST_implement" },
      perm("implement:1", "read", true, "read inside worktree", iso(-40)),
      perm("implement:1", "glob", true, "search inside worktree", iso(-35)),
      perm("implement:1", "shell", false, 'shell metacharacters not allowed: "ls -la a && b"', iso(-30)),
      perm("implement:1", "edit", true, "write inside worktree", iso(-20)),
    ]);
    put(home, "status.json", { state: "running", run: "r-active", card: "BII-TEST-ACTIVE", title: "TEST DATA · Unit tests for policy.py", stage: "implement:1", round: 1, updatedAt: iso(-2), logPath: log });
    return {
      strip: "Working · Implementing · Implementer",
      workerTag: "Implementer · BII-TEST-ACTIVE",
      implementFacts: "✓ 3 · × 1",
      preflightFacts: "✓ 1 · × 0 · 18.82K tokens",
      checksBadge: "Sandboxed checks · Unknown",
      inspect: { station: "implement", texts: ["session ses_TEST_implement", "× Rejected: shell", "3 allowed · 1 rejected", "Unavailable · reported when the stage finishes"] },
      activeStations: 1,
    };
  },
  guardrails(home) {
    put(home, "usage.json", { day: iso().slice(0, 10), today: 1700000, cards: { "BII-TEST-GATES": 90000 } });
    const log = runLog(home, "BII-TEST-GATES", "r-gates", [
      { kind: "start", ts: iso(-90), msg: "claimed (TEST DATA)" },
      { kind: "stage", stage: "implement:1", ts: iso(-80), msg: "session ses_TEST_implement" },
      { kind: "stage-done", stage: "implement:1", ts: iso(-20), msg: "Created tests", data: { billable: 52000 } },
    ]);
    put(home, "status.json", { state: "running", run: "r-gates", card: "BII-TEST-GATES", title: "TEST DATA · Deterministic guardrails", stage: "implement:1", round: 1, updatedAt: iso(-2), logPath: log });
    return {
      strip: "Between reported stages · implement:1 finished, next step not logged",
      checksBadge: "Sandboxed checks · Unknown",
      gateBadges: ["Scope gate · Unknown", "Sensitive/CI gate · Unknown", "Diff evidence gate · Unknown"],
      workerTag: "Last reported · BII-TEST-GATES",
      activeStations: 0,
      noReviewerCharacters: true,
    };
  },
  blocked(home) {
    put(home, "usage.json", { day: iso().slice(0, 10), today: 1900000, cards: { "BII-TEST-BLOCKED": 300000 } });
    const log = runLog(home, "BII-TEST-BLOCKED", "r-blocked", [
      { kind: "start", ts: iso(-120), msg: "claimed (TEST DATA)" },
      { kind: "stage", stage: "implement:3", ts: iso(-60), msg: "session ses_TEST_implement" },
      { kind: "stage-done", stage: "implement:3", ts: iso(-30), msg: "Tried again", data: { billable: 40000 } },
      { kind: "end", ts: iso(-3), msg: "still failing after 3 coding rounds", data: { kind: "blocked", summary: "still failing checks or gates after 3 rounds", rounds: 3 } },
    ]);
    put(home, "status.json", { state: "running", run: "r-blocked", card: "BII-TEST-BLOCKED", title: "TEST DATA · Blocked card", stage: "implement:3", round: 3, updatedAt: iso(-10), logPath: log });
    return { strip: "On hold · BII-TEST-BLOCKED", onHoldShelfCard: "BII-TEST-BLOCKED", workerTagHidden: true };
  },
  stopped(home) {
    put(home, "usage.json", { day: iso().slice(0, 10), today: 1650000, cards: { "BII-TEST-STOP": 22000 } });
    const log = runLog(home, "BII-TEST-STOP", "r-stop", [
      { kind: "start", ts: iso(-50), msg: "claimed (TEST DATA)" },
      { kind: "stage", stage: "preflight", ts: iso(-45), msg: "session ses_TEST_preflight" },
    ]);
    put(home, "status.json", { state: "running", run: "r-stop", card: "BII-TEST-STOP", title: "TEST DATA · Stopped while running", stage: "preflight", updatedAt: iso(-2), logPath: log });
    writeFileSync(join(home, "STOP"), `${iso()} TEST DATA stop\n`, { mode: 0o600 });
    return { strip: "Stopped · STOP set · execution still reported running", preflightBadge: "Last reported: preflight" };
  },
};

// Writes workers.json + status.json exactly as Engine.persistStatusesLocked
// does: one timestamp for both; status.json is the "N lanes active" summary.
const writeLanes = (home, stamp, lanes) => {
  put(home, "workers.json", { version: 1, updatedAt: stamp, workers: lanes.map((l) => ({ ...l, updatedAt: stamp })) });
  put(home, "status.json", { state: "running", updatedAt: stamp, note: `${lanes.length} lanes active; see workers.json` });
};
const laneEntry = (lane, run, card, title, stage, round, logPath, tokens) =>
  ({ lane, state: "running", run, card, title, stage, round, tokens, logPath });

fixtures.lanes = (home) => {
  const stamp = iso(-3);
  put(home, "usage.json", { day: iso().slice(0, 10), today: 1700000, cards: { "BII-TEST-BE": 52000, "BII-TEST-FE": 19000 } });
  const be = runLog(home, "BII-TEST-BE", "r-be", [
    { kind: "start", ts: iso(-60), msg: "claimed (TEST DATA)" },
    { kind: "stage", stage: "implement:1", ts: iso(-50), msg: "session ses_TEST_be" },
    perm("implement:1", "read", true, "read inside worktree", iso(-45)),
    perm("implement:1", "glob", true, "search inside worktree", iso(-40)),
    perm("implement:1", "shell", false, 'shell metacharacters not allowed: "ls -la a && b"', iso(-35)),
    perm("implement:1", "edit", true, "write inside worktree", iso(-30)),
  ]);
  const fe = runLog(home, "BII-TEST-FE", "r-fe", [
    { kind: "start", ts: iso(-40), msg: "claimed (TEST DATA)" },
    { kind: "stage", stage: "preflight", ts: iso(-30), msg: "session ses_TEST_fe" },
    perm("preflight", "read", true, "read inside worktree", iso(-25)),
  ]);
  writeLanes(home, stamp, [
    laneEntry("backend", "r-be", "BII-TEST-BE", "TEST DATA · Backend lane card", "implement:1", 1, be, 52000),
    laneEntry("frontend", "r-fe", "BII-TEST-FE", "TEST DATA · Frontend lane card", "preflight", 1, fe, 19000),
  ]);
  return {
    strip: "Working · 2 lanes in parallel (workers.json) · backend · Implementing | frontend · Pre-flight",
    activeStations: 2,
    lanes: {
      rows: 2, workers: 2, singleWorkerHidden: true, stage: "2 lanes active", noWarning: true,
      badges: { implement: (w) => (w >= 1100 ? "Working" : "Working · backend"), preflight: (w) => (w >= 1100 ? "Working" : "Working · frontend") },
      usage: { backend: "This card: 52K / 2M", frontend: "This card: 19K / 2M" },
      cardMeter: "Per lane",
      noFinishedHistory: true,
      ticker: ["[backend] Allowed: read", "[backend] Rejected: shell", "[frontend] Stage: preflight"],
      inspectLane: { lane: "backend", has: ["Lane backend · BII-TEST-BE", "session ses_TEST_be", "× Rejected: shell", "3 allowed · 1 rejected"], hasNot: ["Lane frontend"] },
    },
    inspect: { station: "implement", texts: ["Lane backend · BII-TEST-BE", "session ses_TEST_be", "× Rejected: shell"] },
  };
};
fixtures["lanes-same-desk"] = (home) => {
  const stamp = iso(-3);
  put(home, "usage.json", { day: iso().slice(0, 10), today: 1700000, cards: {} });
  const a = runLog(home, "BII-TEST-BE", "r-be", [
    { kind: "stage", stage: "implement:1", ts: iso(-50), msg: "session ses_TEST_be" },
    perm("implement:1", "read", true, "read inside worktree", iso(-45)),
  ]);
  const b = runLog(home, "BII-TEST-FE", "r-fe", [
    { kind: "stage", stage: "implement:2", ts: iso(-40), msg: "session ses_TEST_fe" },
    perm("implement:2", "edit", true, "write inside worktree", iso(-35)),
    perm("implement:2", "shell", false, "command not on allowlist", iso(-34)),
  ]);
  writeLanes(home, stamp, [
    laneEntry("backend", "r-be", "BII-TEST-BE", "TEST DATA · Backend", "implement:1", 1, a, 3000),
    laneEntry("frontend", "r-fe", "BII-TEST-FE", "TEST DATA · Frontend", "implement:2", 2, b, 4000),
  ]);
  return {
    strip: "Working · 2 lanes in parallel (workers.json) · backend · Implementing | frontend · Implementing",
    activeStations: 1,
    lanes: { rows: 2, workers: 2, badges: { implement: (w) => (w >= 1100 ? "Working · 2 lanes" : "Working · 2 lanes (backend, frontend)") }, implementFacts: "2 lanes", spread: true, tagsStacked: true },
  };
};
fixtures["lanes-unavailable"] = (home) => {
  put(home, "usage.json", { day: iso().slice(0, 10), today: 1700000, cards: {} });
  // workers.json is OLDER than status.json: a later single-worker write.
  put(home, "workers.json", { version: 1, updatedAt: iso(-600), workers: [
    laneEntry("backend", "r-old1", "BII-OLD-1", "old", "implement:1", 1, "", 1),
    laneEntry("frontend", "r-old2", "BII-OLD-2", "old", "preflight", 1, "", 1),
  ] });
  put(home, "status.json", { state: "running", updatedAt: iso(-2), note: "2 lanes active; see workers.json" });
  return {
    strip: "Running · Stage unavailable",
    activeStations: 0,
    lanes: { rows: 0, workers: 0, warning: "Lane details unavailable · workers.json is older than status.json" },
  };
};

function serve(home) {
  return new Promise((resolve, reject) => {
    const p = spawn(BIN, ["--home", home, "--test-data", "--bind", "127.0.0.1:0"]);
    let buf = "";
    const onData = (d) => {
      buf += d;
      const m = /http:\/\/127\.0\.0\.1:\d+/.exec(buf);
      if (m) resolve({ url: m[0], proc: p });
    };
    p.stdout.on("data", onData);
    p.stderr.on("data", onData);
    p.on("exit", (c) => reject(new Error(`officeview exited ${c}: ${buf}`)));
  });
}

const failures = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "PASS" : "FAIL"} ${name}${detail ? " — " + detail : ""}`);
  if (!ok) failures.push(name);
};

const browser = await chromium.launch({ executablePath: process.env.CHROME_BINARY || "/usr/bin/google-chrome", args: ["--headless=new"] });
const shots = [];
for (const [name, build] of Object.entries(fixtures)) {
  for (const width of [1440, 390]) {
    for (const theme of ["light", "dark"]) {
      for (const reduced of name === "active" ? [false, true] : [false]) {
        const home = mkdtempSync(join(tmpdir(), `office-${name}-`));
        const expect = build(home);
        const { url, proc } = await serve(home);
        const page = await browser.newPage({ viewport: { width, height: width === 1440 ? 1000 : 844 }, deviceScaleFactor: 1 });
        const external = [];
        page.on("request", (r) => {
          const u = new URL(r.url());
          if (!["127.0.0.1", ""].includes(u.hostname) && u.protocol !== "data:") external.push(r.url());
        });
        await page.addInitScript((t) => localStorage.setItem("office-theme", t), theme);
        await page.emulateMedia({ reducedMotion: reduced ? "reduce" : "no-preference" });
        await page.goto(url);
        await page.waitForFunction(() => !document.getElementById("office-state").textContent.includes("Waiting"));
        await page.waitForTimeout(1200);
        const tag = `${name} ${width} ${theme}${reduced ? " reduced" : ""}`;
        const strip = await page.textContent("#office-state");
        check(`${tag}: strip`, strip.includes(expect.strip), strip.trim());
        if (expect.stripContext) {
          check(`${tag}: status headline includes clarification reason`, strip.includes(expect.stripContext), strip.trim());
          check(`${tag}: activity explains Questions asked`, (await page.textContent("#events")).includes(`Questions asked · ${expect.stripContext}`));
        }
        check(`${tag}: TEST DATA label visible`, await page.isVisible("#test-label"));
        if (expect.activity) check(`${tag}: last-run activity`, (await page.textContent("#activity-status")).includes(expect.activity));
        if (expect.shelvesSubtitle) check(`${tag}: shelves subtitle`, (await page.textContent("#shelves")).includes(expect.shelvesSubtitle));
        if (expect.reviewShelfCard) check(`${tag}: review shelf from today's logs`, (await page.textContent("#shelves .success-outcome")).includes(expect.reviewShelfCard));
        if (expect.clarification) {
          const x = expect.clarification;
          const item = page.locator("#shelves .shelf").nth(1).locator(".card-chip", { hasText: x.card });
          const details = item.locator(".clarification-context");
          const actual = await details.textContent();
          for (const part of x.texts)
            check(`${tag}: clarification includes ${part}`, actual.includes(part), actual);
          check(`${tag}: clarification questions numbered`, (await details.locator("li").count()) === x.questions);
          check(`${tag}: no injected clarification HTML`, (await details.locator("script").count()) === 0 && actual.includes(x.escapedHTML));
        }
        if (expect.legacyClarification) {
          const item = page.locator("#shelves .shelf").nth(1).locator(".card-chip", { hasText: expect.legacyClarification });
          check(`${tag}: legacy clarification explicitly unavailable`, (await item.locator(".clarification-context").textContent()).includes("were not recorded for this run"));
        }
        if (expect.onHoldShelfCard) check(`${tag}: on-hold shelf`, (await page.locator("#shelves .shelf").nth(2).textContent()).includes(expect.onHoldShelfCard));
        if (expect.workerTagHidden) check(`${tag}: no worker label when idle`, await page.isHidden("#worker-tag"));
        if (expect.workerTag) {
          check(`${tag}: worker label text`, (await page.textContent("#worker-tag")) === expect.workerTag);
          check(`${tag}: worker label shown only on desktop`, (await page.isVisible("#worker-tag")) === (width >= 1100));
        }
        if (expect.implementFacts) check(`${tag}: implement desk facts`, (await page.textContent("#station-implement .facts")) === expect.implementFacts);
        if (expect.preflightFacts) check(`${tag}: pre-flight desk facts`, (await page.textContent("#station-preflight .facts")) === expect.preflightFacts);
        if (expect.checksBadge) check(`${tag}: checks stay unknown`, (await page.textContent("#station-checks .badge")).includes(expect.checksBadge));
        if (expect.preflightBadge) check(`${tag}: stopped desk last reported`, (await page.textContent("#station-preflight .badge")).includes(expect.preflightBadge));
        if (expect.activeStations !== undefined) check(`${tag}: active desks`, (await page.locator(".station.active").count()) === expect.activeStations);
        if (expect.gateBadges) {
          const gateText = await page.locator("#station-scope .badge, #station-sensitive .badge, #station-diff .badge").allTextContents();
          check(
            `${tag}: deterministic gates stay unknown without telemetry`,
            expect.gateBadges.every((want, index) => gateText[index]?.includes(want)),
            gateText.join(" | "),
          );
        }
        if (expect.lanes) {
          const L = expect.lanes;
          if (L.rows !== undefined) check(`${tag}: lane rows`, (await page.locator(".lane-row").count()) === L.rows);
          if (L.workers !== undefined) check(`${tag}: lane workers`, (await page.locator(".lane-worker").count()) === L.workers);
          if (L.singleWorkerHidden) check(`${tag}: single worker hidden in lane mode`, await page.isHidden("#worker"));
          if (L.stage) check(`${tag}: HUD stage`, (await page.textContent("#stage")).trim() === L.stage);
          if (L.noWarning) check(`${tag}: no lane warning`, await page.isHidden("#lanes-warning"));
          if (L.warning) {
            check(`${tag}: lane warning shown`, await page.isVisible("#lanes-warning"));
            check(`${tag}: lane warning text`, (await page.textContent("#lanes-warning")).includes(L.warning), (await page.textContent("#lanes-warning")).trim());
          }
          for (const [st, want] of Object.entries(L.badges || {})) {
            const text = typeof want === "function" ? want(width) : want;
            const got = (await page.textContent(`#station-${st} .badge`)).trim();
            // Exact match after the leading icon: a short badge must not hide names or extra words.
            check(`${tag}: ${st} desk badge`, got.replace(/^\S+\s/, "") === text, got);
          }
          for (const [lane, text] of Object.entries(L.usage || {}))
            check(`${tag}: ${lane} card usage`, (await page.locator(".lane-row", { hasText: lane }).first().textContent()).includes(text));
          if (L.cardMeter) check(`${tag}: card meter says per lane`, (await page.textContent("#card-meter")).includes(L.cardMeter));
          if (L.noFinishedHistory) check(`${tag}: no finished-run label while lanes run`, !(await page.textContent("#activity-status")).includes("finished run history"));
          if (L.tagsStacked && width >= 1100) {
            const ys = await page.locator(".lane-tag:visible").evaluateAll((n) => n.map((e) => e.getBoundingClientRect().y));
            check(`${tag}: shared-desk labels do not overlap`, ys.length === 2 && Math.abs(ys[0] - ys[1]) >= 20, ys.join(", "));
          }
          if (L.implementFacts) check(`${tag}: shared desk facts`, (await page.textContent("#station-implement .facts")).trim() === L.implementFacts);
          const ticker = await page.textContent("#events");
          for (const t of L.ticker || []) check(`${tag}: ticker "${t}"`, ticker.includes(t));
          if (L.spread) {
            const xs = await page.locator(".lane-worker").evaluateAll((n) => n.map((e) => e.getBoundingClientRect().x));
            check(`${tag}: lanes sharing a desk are spaced apart`, xs.length === 2 && Math.abs(xs[0] - xs[1]) >= 20, xs.join(", "));
          }
          if (L.workers) {
            const visibleTags = await page.locator(".lane-tag:visible").count();
            check(`${tag}: lane labels shown only on desktop`, visibleTags === (width >= 1100 ? L.workers : 0), `${visibleTags}`);
          }
          if (L.inspectLane) {
            const btn = page.locator(".lane-row button", { hasText: `Inspect ${L.inspectLane.lane}` });
            await btn.click();
            await page.waitForSelector("#inspector[open]");
            const body = await page.textContent("#inspector");
            for (const t of L.inspectLane.has) check(`${tag}: lane inspector shows "${t}"`, body.includes(t));
            for (const t of L.inspectLane.hasNot) check(`${tag}: lane inspector omits "${t}"`, !body.includes(t));
            await page.keyboard.press("Escape");
            await page.waitForFunction(() => !document.getElementById("inspector").open);
            check(`${tag}: Escape returns focus to the lane button`, await btn.evaluate((n) => document.activeElement === n));
          }
        }
        if (expect.noReviewerCharacters)
          check(`${tag}: no internal reviewer characters`, (await page.locator(".reviewer-character, .reviewer-pose").count()) === 0);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
        check(`${tag}: no horizontal overflow`, overflow <= 0, `${overflow}px`);
        const file = `${OUT}/office-${name}-${width}-${theme}${reduced ? "-reduced" : ""}.png`;
        await page.screenshot({ path: file, fullPage: true });
        shots.push(file);
        if (expect.inspect && !reduced) {
          const trigger = page.locator(`#station-${expect.inspect.station} .desk-name`);
          await trigger.click();
          await page.waitForSelector("#inspector[open]");
          const body = await page.textContent("#inspector");
          for (const t of expect.inspect.texts) check(`${tag}: inspector shows "${t}"`, body.includes(t));
          const inspectFile = `${OUT}/office-${name}-${width}-${theme}-inspector.png`;
          await page.screenshot({ path: inspectFile });
          shots.push(inspectFile);
          await page.keyboard.press("Escape");
          await page.waitForFunction(() => !document.getElementById("inspector").open);
          check(`${tag}: Escape closes inspector and returns focus`, await trigger.evaluate((n) => document.activeElement === n));
        }
        check(`${tag}: no external requests`, external.length === 0, external.join(", "));
        await page.close();
        proc.kill();
      }
    }
  }
}
await browser.close();
console.log(`\n${shots.length} screenshots written to ${OUT}`);
console.log(failures.length ? `FAILURES: ${failures.length}\n- ${failures.join("\n- ")}` : "ALL CHECKS PASSED");
process.exit(failures.length ? 1 : 0);
