// Runs the real server against explicitly labeled temporary telemetry.
// PLAYWRIGHT_MODULE points to a separately installed Playwright module.
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, access } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { pathToFileURL } from "node:url";
const { chromium } = await import(
  pathToFileURL(
    process.env.PLAYWRIGHT_MODULE ||
      "/tmp/officeview-browser/node_modules/playwright/index.mjs",
  )
);
const output = resolve(
  process.env.OFFICEVIEW_SHOTS || "docs/graphical-office/evidence",
);
await mkdir(output, { recursive: true });
const home = await mkdtemp(join(tmpdir(), "officeview-proof-"));
const binary =
  process.env.OFFICEVIEW_BINARY || "/tmp/officeview-browser/officeview";
const child = spawn(binary, ["--home", home, "--controls", "--test-data"], {
  stdio: ["ignore", "pipe", "pipe"],
});
let url = "";
let serverErrors = "";
child.stderr.on("data", (b) => (serverErrors += b));
const origin = await new Promise((resolve, reject) => {
  child.stdout.on("data", (b) => {
    url += b;
    const m = url.match(/http:\/\/127\.0\.0\.1:\d+/);
    if (m) resolve(m[0]);
  });
  child.on("exit", (code) =>
    reject(Error(`server exited ${code}: ${serverErrors}`)),
  );
  setTimeout(() => reject(Error("server start timeout")), 10000).unref();
});
let browser;
const report = [];
const record = (text) => {
  console.log(text);
  report.push(text);
};
const atomic = async (name, value) => {
  const { rename } = await import("node:fs/promises");
  const path = join(home, name);
  await mkdir(resolve(path, ".."), { recursive: true });
  await writeFile(path + ".tmp", JSON.stringify(value) + "\n");
  await rename(path + ".tmp", path);
};
let serial = 0;
const base = () => ({
  state: "running",
  card: "BII-TEST",
  title: "TEST DATA · Policy unit tests",
  stage: "implement:2",
  round: 2,
  tokens: 1234,
  updatedAt: new Date().toISOString(),
});
async function fixture({
  status = base(),
  events = [],
  usage,
  stop = false,
  config,
} = {}) {
  const { rm } = await import("node:fs/promises");
  const run = `test-${++serial}`,
    logPath = `runs/${status?.card || "TEST"}/${run}.jsonl`;
  if (status) {
    status = { ...status, run, logPath };
    await atomic("status.json", status);
  } else await rm(join(home, "status.json"), { force: true });
  const source = events.map((e) => ({
    run,
    card: status?.card || "BII-TEST",
    ts: new Date().toISOString(),
    ...e,
  }));
  await mkdir(resolve(join(home, logPath), ".."), { recursive: true });
  await writeFile(
    join(home, logPath),
    source.map((e) => JSON.stringify(e) + "\n").join(""),
  );
  const now = new Date();
  const day = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
  await atomic(
    "usage.json",
    usage ?? { day, today: 1600000, cards: { "BII-TEST": 1234 } },
  );
  await atomic(
    "config.json",
    config ?? {
      daily_tokens: 10000000,
      per_card_tokens: 2000000,
      boards: [{ name: "TEST DATA board" }],
    },
  );
  if (stop) await writeFile(join(home, "STOP"), "TEST DATA\n");
  else await rm(join(home, "STOP"), { force: true });
}
try {
  browser = await chromium.launch({
    executablePath: process.env.CHROME_BINARY || "/usr/bin/google-chrome",
    headless: true,
    args: ["--no-sandbox"],
  });
  const checks = [];
  const captureWidths = process.env.OFFICEVIEW_INTERACTIONS_ONLY
    ? []
    : [1440, 390];
  for (const width of captureWidths)
    for (const theme of ["light", "dark"])
      for (const reduce of [false, true]) {
        await fixture({
          events: [
            { kind: "start" },
            { kind: "stage", stage: "implement:2" },
            { kind: "permission", data: { action: "edit", allow: true } },
          ],
        });
        const context = await browser.newContext({
          viewport: { width, height: width === 1440 ? 1000 : 844 },
          colorScheme: theme,
          reducedMotion: reduce ? "reduce" : "no-preference",
        });
        const page = await context.newPage(),
          errors = [],
          requests = [];
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("request", (r) => requests.push(r.url()));
        await page.goto(origin);
        await page.getByText("TEST DATA", { exact: true }).waitFor();
        await page
          .locator("#stage")
          .filter({ hasText: "Implementing" })
          .waitFor();
        await page.waitForFunction(
          () => document.querySelector("#connection").hidden,
        );
        assert.equal(
          await page.locator("#round").textContent(),
          "Round 2 of 3",
        );
        assert.equal(
          await page.locator("#station-checks .badge").textContent(),
          "? Sandboxed checks · Unknown",
        );
        assert.equal(await page.locator("#worker .carried").count(), 0); // atlas carries symbols, never duplicate worker DOM
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth,
          ),
          false,
        );
        const geometry = await page.evaluate(() => {
          const r = document.querySelector(".floor").getBoundingClientRect();
          const list = [...document.querySelectorAll(".station")].map((n) => ({
            id: n.id,
            ...Object.fromEntries(
              ["x", "y", "width", "height"].map((k) => [
                k,
                n.getBoundingClientRect()[k],
              ]),
            ),
          }));
          return { floor: { x: r.x, width: r.width, height: r.height }, list };
        });
        if (width === 1440) {
          assert.equal(geometry.floor.x, 24);
          assert.equal(geometry.floor.width, 1392);
          assert.equal(geometry.floor.height, 560);
          assert.equal(
            geometry.list.find((n) => n.id === "station-claim").height,
            88,
          );
        }
        const hitErrors = await page
          .locator("button:visible,select:visible,summary:visible")
          .evaluateAll((nodes) =>
            nodes
              .filter((n) => {
                const b = n.getBoundingClientRect();
                return b.width < 44 || b.height < 44;
              })
              .map((n) => n.textContent),
          );
        assert.deepEqual(hitErrors, [], "44px controls");
        if (reduce)
          assert.equal(
            await page.evaluate(() => document.getAnimations().length),
            0,
          );
        await page.screenshot({
          path: join(
            output,
            `baseline-${width}-${theme}${reduce ? "-reduced" : ""}.png`,
          ),
          fullPage: true,
        });
        assert.deepEqual(errors, []);
        assert(
          requests.every((u) => u.startsWith(origin + "/")),
          "external request",
        );
        checks.push(
          `${width}/${theme}/${reduce ? "reduced" : "auto"}: layout, labels, controls, network, screenshot PASS`,
        );
        await context.close();
      }
  checks.forEach(record);
  const cases = [
    ["idle", { status: { ...base(), state: "idle" } }, "Idle"],
    ["implement-round-2", {}, "Implementing"],
    [
      "legacy-review-stage",
      {
        status: { ...base(), stage: "review:acceptance-criteria", round: 1 },
        events: [
          { kind: "stage-done", stage: "review:acceptance-criteria" },
          { kind: "verdict", data: { result: "pass" } },
        ],
      },
      "Stage unavailable",
    ],
    [
      "ready-for-review",
      {
        events: [
          {
            kind: "end",
            data: {
              kind: "ready-for-review",
              pr: "https://github.com/example/repo/pull/1",
            },
          },
        ],
      },
      "Draft PR ready",
    ],
    [
      "needs-clarification",
      { events: [{ kind: "end", data: { kind: "needs-clarification" } }] },
      "Questions asked",
    ],
    [
      "blocked",
      { events: [{ kind: "end", data: { kind: "blocked" } }] },
      "Blocked",
    ],
    ["stopped-running", { stop: true }, "Implementing"],
    [
      "near-cap",
      {
        usage: {
          day: new Date().toISOString().slice(0, 10),
          today: 8000000,
          cards: { "BII-TEST": 1600000 },
        },
      },
      "Implementing",
    ],
    [
      "capped",
      {
        usage: {
          day: new Date().toISOString().slice(0, 10),
          today: 12000000,
          cards: { "BII-TEST": 2400000 },
        },
      },
      "Implementing",
    ],
    ["missing-data", { status: null, usage: {} }, "Waiting for status"],
    [
      "error",
      { events: [{ kind: "error", msg: "TEST DATA · sandbox check failed" }] },
      "Implementing",
    ],
    [
      "stale",
      {
        status: {
          ...base(),
          updatedAt: new Date(Date.now() - 120000).toISOString(),
        },
      },
      "Implementing",
    ],
    [
      "long-title",
      {
        status: {
          ...base(),
          title: "TEST DATA " + "very-long-title-with-no-space".repeat(24),
        },
      },
      "Implementing",
    ],
    ["round-mismatch", { status: { ...base(), round: 1 } }, "Implementing"],
    [
      "unknown-stage",
      { status: { ...base(), stage: "constructor", round: 99 } },
      "Stage unavailable",
    ],
    [
      "invalid-cap",
      { config: { daily_tokens: -1, per_card_tokens: "bad" } },
      "Implementing",
    ],
  ];
  for (const width of captureWidths)
    for (const theme of ["light", "dark"]) {
      const context = await browser.newContext({
        viewport: { width, height: width === 1440 ? 1000 : 844 },
        colorScheme: theme,
        reducedMotion: "reduce",
      });
      const page = await context.newPage();
      for (const [name, data, stage] of cases) {
        await fixture(data);
        await page.goto(origin);
        await page.waitForFunction(
          (want) => document.querySelector("#stage").textContent === want,
          stage,
        );
        await page.waitForTimeout(80);
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth,
          ),
          false,
          `${name}: overflow`,
        );
        if (name === "idle") {
          assert.equal(
            await page.locator("#no-card").textContent(),
            "No active card",
          );
          assert.equal(await page.locator(".station.active").count(), 0);
        }
        if (name === "stopped-running" || name === "capped")
          await page
            .getByText("Execution still reported running", { exact: true })
            .waitFor();
        if (name === "legacy-review-stage") {
          assert.equal(await page.locator(".reviewer-character").count(), 0);
          assert((await page.locator("#station-scope .badge").textContent()).includes("Scope gate · Unknown"));
        }
        if (name === "round-mismatch")
          assert.equal(
            await page.locator("#round").textContent(),
            "Round data mismatch",
          );
        await page.screenshot({
          path: join(output, `state-${name}-${width}-${theme}.png`),
          fullPage: true,
        });
      }
      record(`${width}/${theme}: ${cases.length} evidence-state captures PASS`);
      await context.close();
    }
  await fixture({
    events: Array.from({ length: 30 }, (_, i) => ({
      kind: "permission",
      data: { action: `TEST DATA action ${i}`, allow: i % 2 === 0 },
    })),
  });
  const context = await browser.newContext({
      viewport: { width: 390, height: 844 },
      reducedMotion: "reduce",
    }),
    page = await context.newPage();
  await page.goto(origin);
  await page.waitForFunction(
    () => document.querySelector("#control").disabled === false,
  );
  await page.locator("#control").click();
  await page.locator("#confirm").waitFor({ state: "visible" });
  assert.equal(await page.evaluate(() => document.activeElement.id), "cancel");
  await page.keyboard.press("Shift+Tab");
  assert.equal(
    await page.evaluate(() => document.activeElement.id),
    "confirm-action",
  );
  await page.keyboard.press("Escape");
  assert.equal(await page.evaluate(() => document.activeElement.id), "control");
  await page.locator("#control").click();
  await page.locator("#confirm-action").click();
  await page.getByText("Stopped · STOP set", { exact: true }).first().waitFor();
  await access(join(home, "STOP"));
  await page.locator("#control").click();
  await page.locator("#confirm-action").click();
  await page.waitForFunction(
    () =>
      document.querySelector("#stop-label").textContent ===
      "Running · STOP clear",
  );
  assert.equal(
    await page.evaluate(() => document.activeElement.id),
    "control",
    "confirmed action returns focus",
  );
  await assert.rejects(access(join(home, "STOP")));
  const initialVisible = await page.locator("#events>li:visible").count();
  assert.equal(initialVisible, 5);
  await page.locator("#older").click();
  assert.equal(await page.locator("#events>li:visible").count(), 25);
  await page
    .locator("#events>li:visible details")
    .first()
    .locator("summary")
    .click();
  assert(await page.locator("#events>li:visible pre").first().isVisible());
  await page.selectOption("#theme", "dark");
  await page.selectOption("#motion", "off");
  await page.reload();
  assert.equal(await page.locator("#theme").inputValue(), "dark");
  assert.equal(await page.locator("#motion").inputValue(), "off");
  for (const width of [320, 640, 900, 1099]) {
    await page.setViewportSize({ width, height: 844 });
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        ),
    );
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
      `overflow ${width}`,
    );
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => (document.documentElement.style.zoom = "2"));
  await page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
  if (
    await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)
  ) {
    console.log(
      await page.locator("body *").evaluateAll((nodes) =>
        nodes
          .filter((n) => n.getBoundingClientRect().right > innerWidth + 1)
          .map((n) => ({
            tag: n.tagName,
            id: n.id,
            cls: n.className.baseVal ?? n.className,
            right: n.getBoundingClientRect().right,
            width: n.getBoundingClientRect().width,
          }))
          .slice(0, 30),
      ),
    );
  }
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
    "200% zoom overflow",
  );
  await page.evaluate(() => (document.documentElement.style.zoom = "1"));
  await fixture({
    status: {
      ...base(),
      title:
        'TEST DATA <img src="https://invalid.test/" onerror="window.injected=true">' +
        " long escaped title".repeat(30),
    },
  });
  await page.reload();
  await page.waitForFunction(() =>
    document.querySelector("#card-title").textContent.includes("<img"),
  );
  assert.equal(await page.locator("#card img").count(), 0);
  assert.equal(await page.evaluate(() => window.injected), undefined);
  await page.locator("#title-disclosure summary").click();
  assert(await page.locator("#full-title").isVisible());
  await page.route("**/api/state", (route) => route.abort());
  await page.waitForFunction(() =>
    document
      .querySelector("#connection")
      .textContent.includes("Connection lost"),
  );
  assert((await page.locator("#card-key").textContent()).includes("BII-TEST"));
  assert.equal(await page.evaluate(() => document.getAnimations().length), 0);
  await page.unroute("**/api/state");
  record(
    "Controls: cancel focus, focus trap, Escape, STOP set/readback, resume/readback PASS",
  );
  record(
    "Disclosures/history, persisted preferences, 320/640/900/1099px, 200% zoom, escaped XSS, last-known disconnected state PASS",
  );
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.selectOption("#motion", "auto");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await fixture({
    status: { ...base(), stage: "claim", round: 1 },
    events: [{ kind: "start" }],
  });
  await page.reload();
  await page.waitForFunction(
    () =>
      document.querySelector("#stage").textContent === "Claiming card" &&
      document.querySelector("#connection").hidden,
  );
  assert.equal(
    await page.evaluate(
      () =>
        document
          .getAnimations()
          .filter((a) => a.effect?.target?.id === "worker").length,
    ),
    0,
    "no replay on load",
  );
  const liveStatus = JSON.parse(
    await readFile(join(home, "status.json"), "utf8"),
  );
  await atomic("status.json", {
    ...liveStatus,
    stage: "setup",
    updatedAt: new Date().toISOString(),
  });
  await page.waitForFunction(() =>
    document.getAnimations().some((a) => a.effect?.target?.id === "worker"),
  );
  const travel = await page.evaluate(() => {
    const animation = document
      .getAnimations()
      .find((a) => a.effect?.target?.id === "worker");
    return {
      duration: animation.effect.getTiming().duration,
      frames: animation.effect.getKeyframes(),
    };
  });
  assert.equal(travel.duration, 700);
  assert(
    travel.frames.every((f) => f.top === "112px"),
    "adjacent aisle",
  );
  await page.waitForTimeout(1300);
  assert.equal(
    await page.evaluate(() => document.getAnimations().length),
    0,
    "finite arrival cycle",
  );
  await page.waitForTimeout(2100);
  assert.equal(
    await page.evaluate(() => document.getAnimations().length),
    0,
    "identical snapshot does not restart",
  );
  const { appendFile } = await import("node:fs/promises");
  await appendFile(
    join(home, liveStatus.logPath),
    JSON.stringify({
      kind: "permission",
      run: liveStatus.run,
      card: liveStatus.card,
      data: { allow: false, action: "TEST DATA rejected edit" },
    }) + "\n",
  );
  await page
    .getByText("Rejected: TEST DATA rejected edit", { exact: true })
    .waitFor();
  assert.equal(await page.locator("#stage").textContent(), "Setting up");
  await atomic("status.json", {
    ...liveStatus,
    stage: "setup",
    state: "idle",
    updatedAt: new Date().toISOString(),
  });
  await page.waitForFunction(
    () => document.querySelector("#stage").textContent === "Idle",
  );
  assert.equal(await page.evaluate(() => document.getAnimations().length), 0);
  record(
    "Real-file stage change: 700ms aisle travel, finite arrival pose, no replay on load/identical polls, idle static, rejection not terminal PASS",
  );
  await page.emulateMedia({ reducedMotion: "reduce" });
  await atomic("status.json", {
    ...liveStatus,
    stage: "preflight",
    state: "running",
    updatedAt: new Date().toISOString(),
  });
  await page.waitForFunction(
    () => document.querySelector("#stage").textContent === "Pre-flight",
  );
  assert.equal(await page.evaluate(() => document.getAnimations().length), 0);
  assert.equal(
    await page.locator("#worker use").getAttribute("href"),
    "#worker-think-0",
  );
  await page.route("**/api/stop", (route) =>
    route.fulfill({ status: 503, body: "TEST DATA failed write" }),
  );
  await page.locator("#control").click();
  await page.locator("#confirm-action").click();
  await page
    .getByText("Could not change STOP. Try again.", { exact: true })
    .waitFor();
  assert.equal(
    await page.locator("#stop-label").textContent(),
    "Running · STOP clear",
  );
  await assert.rejects(access(join(home, "STOP")));
  record(
    "Reduced-motion live stage change uses frame 0; failed STOP write keeps previous state PASS",
  );
  await appendFile(
    join(home, liveStatus.logPath),
    JSON.stringify({
      kind: "error",
      run: liveStatus.run,
      card: liveStatus.card,
      ts: new Date().toISOString(),
      msg: "TEST DATA transient error",
    }) + "\n",
  );
  await page
    .locator("#error")
    .filter({ hasText: "TEST DATA transient error" })
    .waitFor();
  await atomic("status.json", {
    ...liveStatus,
    stage: "preflight",
    state: "running",
    updatedAt: new Date().toISOString(),
  });
  await page.waitForFunction(() => document.querySelector("#error").hidden);
  assert.equal(await page.locator("#stage").textContent(), "Pre-flight");
  record(
    "New confirmed snapshot clears the earlier error condition while preserving the reported stage PASS",
  );
  await context.close();
  record(`Screenshots saved to ${output}`);
} finally {
  if (browser) await browser.close();
  child.kill("SIGTERM");
  await writeFile(join(output, "browser-output.txt"), report.join("\n") + "\n");
}
