"use strict";
const $ = (id) => document.getElementById(id);
const el = (tag, text, cls) => {
  const node = document.createElement(tag);
  if (text != null) node.textContent = text;
  if (cls) node.className = cls;
  return node;
};
const ns = "http://www.w3.org/2000/svg";
const svgNode = (tag, attrs = {}) => {
  const n = document.createElementNS(ns, tag);
  for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, v);
  return n;
};
const graphic = (symbol, cls, width, height) => {
  const n = svgNode("svg", {
    viewBox: `0 0 ${width} ${height}`,
    width,
    height,
    "aria-hidden": "true",
    class: cls,
  });
  n.append(svgNode("use", { href: `#${symbol}` }));
  return n;
};
const stored = (key, fallback) => {
  try {
    return localStorage.getItem(key) || fallback;
  } catch {
    return fallback;
  }
};
const save = (key, value) => {
  try {
    localStorage.setItem(key, value);
  } catch {}
};
const osTheme = matchMedia("(prefers-color-scheme: dark)"),
  reduced = matchMedia("(prefers-reduced-motion: reduce)");
const themePreference = stored("office-theme", "system"),
  motionPreference = stored("office-motion", "auto");
$("theme").value = ["system", "light", "dark"].includes(themePreference)
  ? themePreference
  : "system";
$("motion").value = ["auto", "off"].includes(motionPreference)
  ? motionPreference
  : "auto";
function preferences() {
  document.documentElement.dataset.theme =
    $("theme").value === "system"
      ? osTheme.matches
        ? "dark"
        : "light"
      : $("theme").value;
  document.documentElement.dataset.motion = $("motion").value;
}
preferences();
osTheme.addEventListener("change", preferences);
$("theme").addEventListener("change", () => {
  save("office-theme", $("theme").value);
  preferences();
});
$("motion").addEventListener("change", () => {
  save("office-motion", $("motion").value);
  preferences();
  cancelMotion();
  placeWorker();
});
reduced.addEventListener("change", () => {
  cancelMotion();
  placeWorker();
});

// This atlas implements the spec's geometric construction directly. Every
// symbol has an explicit state/frame ID; there are no generated bitmap assets.
function atlas() {
  const defs = $("atlas").querySelector("defs");
  const shape = (parent, tag, attrs) => parent.appendChild(svgNode(tag, attrs));
  const rect = (p, x, y, width, height, rx = 2, cls) =>
    shape(p, "rect", {
      x,
      y,
      width,
      height,
      rx,
      ...(cls ? { class: cls } : {}),
    });
  const path = (p, d, cls) =>
    shape(p, "path", { d, fill: "none", ...(cls ? { class: cls } : {}) });
  for (const kind of [
    "queue",
    "claim",
    "setup",
    "preflight",
    "implement",
    "checks",
    "gate",
    "publish",
    "shelf-empty",
    "shelf-full",
  ]) {
    const s = shape(defs, "symbol", {
      id: `prop-${kind}`,
      viewBox: "0 0 64 40",
    });
    if (kind.startsWith("shelf")) {
      path(s, "M4 4 V36 M60 4 V36 M4 18 H60 M4 34 H60");
      if (kind === "shelf-full") rect(s, 16, 4, 18, 14);
      continue;
    }
    rect(s, 2, 20, 60, 8, 3);
    rect(s, 8, 28, 4, 10, 0);
    rect(s, 52, 28, 4, 10, 0);
    if (kind === "queue")
      for (let i = 0; i < 3; i++) {
        rect(s, 18 + i * 3, 2 + i * 3, 22, 14);
        path(
          s,
          `M${22 + i * 3} ${6 + i * 3} h12 M${22 + i * 3} ${10 + i * 3} h12`,
          "line",
        );
      }
    if (kind === "claim") {
      rect(s, 18, 2, 22, 16);
      path(s, "M23 10 L27 14 L35 6");
    }
    if (kind === "setup" || kind === "implement") {
      rect(s, 14, 1, 36, 16);
      path(s, "M32 17 V20 M23 7 L19 10 L23 13 M41 7 L45 10 L41 13", "line");
    }
    if (kind === "preflight")
      path(
        s,
        "M32 6 Q20 0 10 5 V17 Q22 12 32 18 V6 Q44 0 54 5 V17 Q42 12 32 18",
      );
    if (kind === "checks") {
      rect(s, 10, 7, 44, 12);
      for (const x of [17, 28, 39]) rect(s, x, 10, 6, 6, 0);
    }
    if (kind === "gate") {
      rect(s, 20, 2, 24, 17);
      path(s, "M25 7 H39 M25 11 H39 M25 15 H34", "line");
      path(s, "M36 14 L39 17 L45 9");
    }
    if (kind === "publish") {
      rect(s, 20, 4, 24, 14);
      path(s, "M24 10 H29 M24 14 H40", "line");
      path(s, "M32 12 V0 M27 5 L32 0 L37 5");
    }
  }
  for (const character of ["worker"])
    for (const [state, frames] of Object.entries({
      idle: 1,
      walk: 4,
      work: 2,
      think: 2,
      success: 2,
      fail: 1,
    }))
      for (let frame = 0; frame < frames; frame++) {
        const s = shape(defs, "symbol", {
          id: `${character}-${state}-${frame}`,
          viewBox: "0 0 40 48",
          class: "worker-symbol",
        });
        const g = shape(s, "g");
        shape(g, "circle", { cx: 20, cy: 9, r: 7 });
        rect(g, 11, 18, 18, 16, 5, "body");
        let left = [6, 31],
          right = [34, 31];
        if (state === "walk") {
          left = [6 + [-3, 0, 3, 0][frame], 31];
          right = [34 - [-3, 0, 3, 0][frame], 31];
        }
        if (state === "work") right = [34 - frame * 2, 29];
        if (state === "think") right = [26 + frame, 14];
        if (state === "success") right = [34, 27 + frame * 4];
        path(g, `M12 22 L${left.join(" ")} M28 22 L${right.join(" ")}`, "limb");
        const step = state === "walk" ? [-4, 0, 4, 0][frame] : 0;
        path(g, `M16 34 L${14 + step} 46 M24 34 L${26 - step} 46`, "limb");
        const card = shape(g, "g", { class: "carried" });
        rect(card, 25, 22, 12, 15);
        path(card, "M28 27 H34 M28 31 H34", "paper-line");
      }
}
atlas();
const stationDefs = [
  ["queue", "Autopilot Queue", "queue", 112, 232],
  ["claim", "Claim", "claim", 336, 112],
  ["setup", "Setup", "setup", 560, 112],
  ["preflight", "Pre-flight", "preflight", 784, 112],
  ["implement", "Implement", "implement", 1120, 112],
  ["checks", "Sandboxed checks", "checks", 1120, 232],
  ["scope", "Scope gate", "gate", 224, 352],
  ["sensitive", "Sensitive/CI gate", "gate", 672, 352],
  ["diff", "Diff evidence gate", "gate", 1120, 352],
  ["publish", "Publish", "publish", 672, 472],
];
const stations = new Map();
for (const [id, name, prop, x, y] of stationDefs) {
  const section = el("section", null, `station ${id}`);
  section.id = `station-${id}`;
  section.setAttribute("aria-label", name);
  section.append(graphic(`prop-${prop}`, "prop", 64, 40));
  const copy = el("div", null, "station-copy"),
    heading = el("h3"),
    button = el("button", name, "desk-name"),
    badge = el("p", "? Unknown", "badge"),
    facts = el("p", "", "facts");
  button.setAttribute("aria-haspopup", "dialog");
  button.addEventListener("click", () => openInspector(id, button));
  heading.append(button);
  facts.hidden = true;
  copy.append(heading, badge, facts);
  section.append(copy);
  const item = { id, section, badge, facts, button, x, y, result: "Unknown" };
  if (id === "queue") {
    badge.textContent = "Queue unavailable";
    badge.className = "meta";
  }
  $("stations").insertBefore(section, $("worker"));
  stations.set(id, item);
}
// The DOM follows process order independently of desktop grid placement.
const stageMap = {
  claim: ["claim", "Claiming card", "work"],
  setup: ["setup", "Setting up", "work"],
  preflight: ["preflight", "Pre-flight", "think"],
  publish: ["publish", "Publishing draft PR", "work"],
};
// Who handles each desk. External review is deliberately outside this office.
// Only pre-flight and implementation create model sessions; all guardrails are
// deterministic engine work and stay Unknown without structured telemetry.
const roleOf = {
  queue: "Queue",
  claim: "Autopilot engine",
  setup: "Autopilot engine",
  preflight: "Pre-flight agent",
  implement: "Implementer",
  checks: "Autopilot engine",
  scope: "Autopilot engine",
  sensitive: "Autopilot engine",
  diff: "Autopilot engine",
  publish: "Autopilot engine",
};
const modelRole = new Set(["preflight", "implement"]);
// Steps that write no run-log events today (status.json only, or nothing).
const telemetryGap = {
  queue:
    "Queue unavailable: board lists are not part of the autopilot telemetry. Missing: a queue snapshot (list name, card keys/titles, retrieval time).",
  claim:
    "This step is reported only in status.json while it runs; the run log has no stage event for it, so session, times and activity are unavailable.",
  setup:
    "This step is reported only in status.json while it runs; the run log has no stage event for it, so session, times and activity are unavailable.",
  checks:
    "Sandboxed checks · Unknown: the autopilot runs checks in a sandbox but logs no checks event (no stage, result or exit code), so nothing here can be shown as passed or failed.",
  scope:
    "Scope gate · Unknown: scope and empty-diff results are not emitted as structured run-log fields, so this desk cannot claim pass or fail.",
  sensitive:
    "Sensitive/CI gate · Unknown: sensitive-file and CI-file guardrail results are not emitted as structured run-log fields.",
  diff:
    "Diff evidence gate · Unknown: git diff --check and complete-diff results are included in the evidence packet but are not emitted as structured live telemetry.",
  publish:
    "This step is reported only in status.json while it runs; the run log has no stage event for it, so its PR result appears only in the final outcome.",
};
const textOr = (v, fallback) => (typeof v === "string" && v ? v : fallback);
// Per-desk facts from real run-log records: the latest stage event opens a
// desk (its literal msg names the session), permission records carry their
// stage, and stage-done closes it with billable tokens and the agent summary.
function deskFacts(rows) {
  const facts = new Map();
  for (const row of rows) {
    const n = row.native;
    if (typeof n.stage !== "string") continue;
    const id = stageInfo({ stage: n.stage })?.[0];
    if (!id) continue;
    let f = facts.get(id);
    if (n.kind === "stage" || !f) {
      f = {
        stage: n.stage,
        session: null,
        started: null,
        doneSeen: false,
        done: null,
        billable: null,
        summary: null,
        allowed: 0,
        rejected: 0,
        perms: [],
      };
      facts.set(id, f);
    }
    if (n.kind === "stage") {
      f.session = textOr(n.msg, null);
      f.started = textOr(n.ts, null);
    } else if (n.kind === "permission") {
      if (n.data?.allow === true) f.allowed++;
      else if (n.data?.allow === false) f.rejected++;
      f.perms.push({
        ts: textOr(n.ts, ""),
        action: textOr(n.data?.action, "Unavailable"),
        allow: n.data?.allow,
        reason: textOr(n.msg, ""),
      });
      if (f.perms.length > 10) f.perms.shift();
    } else if (n.kind === "stage-done") {
      f.doneSeen = true;
      f.done = textOr(n.ts, null);
      f.billable = validNumber(n.data?.billable) ? n.data.billable : null;
      f.summary = textOr(n.msg, null);
    }
  }
  return facts;
}
const sortedRows = () => [...events.values()].sort(eventOrder);
const currentRows = () =>
  sortedRows().filter((row) => isCurrent(row, eventContext(row)));
const lastRows = () =>
  snapshot?.lastRun
    ? sortedRows().filter(
        (row) =>
          row.lastRun &&
          row.native.run === snapshot.lastRun.run &&
          row.native.card === snapshot.lastRun.card,
      )
    : [];
const shelves = new Map();
for (const [kind, name, tone] of [
  ["ready-for-review", "Review", "success"],
  ["needs-clarification", "Needs Clarification", "warning"],
  ["blocked", "On Hold", "warning"],
]) {
  const section = el("section", null, `shelf panel ${tone}-outcome`),
    head = el("div", null, "shelf-head"),
    icon = graphic("prop-shelf-empty", "shelf-icon", 64, 40),
    labels = el("div");
  const subtitle = el("p", "This session", "meta");
  labels.append(el("h3", name), subtitle);
  head.append(icon, labels);
  const list = el("div"),
    empty = el("p", "No recorded outcomes", "meta"),
    more = el("button", "Show all");
  more.hidden = true;
  section.append(head, empty, list, more);
  $("shelves").append(section);
  const shelf = {
    kind,
    subtitle,
    section,
    icon,
    list,
    empty,
    more,
    tone,
    items: new Map(),
    expanded: false,
  };
  more.addEventListener("click", () => {
    shelf.expanded = true;
    renderShelf(shelf);
  });
  shelves.set(kind, shelf);
}
const meters = new Map();
for (const [id, label] of [
  ["daily-meter", "Today"],
  ["card-meter", "This card"],
]) {
  const node = $(id),
    heading = el("h2", label),
    total = el("p", "Unavailable", "total"),
    meter = el("meter"),
    unknownTrack = el("div", null, "unknown-meter"),
    meaning = el("p", "Usage unavailable", "meta"),
    details = el("details"),
    summary = el("summary", "Exact usage and cap"),
    exact = el("p");
  meter.setAttribute("aria-label", label);
  meter.min = 0;
  meter.max = 1;
  meter.value = 0;
  meter.hidden = true;
  unknownTrack.setAttribute("aria-hidden", "true");
  details.append(summary, exact);
  node.append(heading, total, meter, unknownTrack, meaning, details);
  meters.set(id, { total, meter, unknownTrack, meaning, exact });
}
const validNumber = (n) => Number.isSafeInteger(n) && n >= 0;
const compact = (n) =>
  new Intl.NumberFormat("en", {
    notation: "compact",
    maximumFractionDigits: 2,
  }).format(n);
function budget(id, value, cap, day, zone) {
  const m = meters.get(id),
    known = validNumber(value),
    capped = validNumber(cap) && cap > 0;
  m.total.textContent = `${known ? compact(value) : "Unavailable"} / ${capped ? compact(cap) : "Unavailable"}`;
  const ratio = known && capped ? value / cap : null;
  m.meter.hidden = ratio === null;
  m.unknownTrack.hidden = ratio !== null;
  m.meter.max = capped ? cap : 1;
  m.meter.value = ratio === null ? 0 : Math.min(value, cap);
  m.meter.className = ratio >= 1 ? "danger" : ratio >= 0.8 ? "warning" : "";
  m.meaning.textContent = !known
    ? "Usage unavailable"
    : !capped
      ? "Cap unavailable"
      : `${Math.round(ratio * 100)}% · ${ratio >= 1 ? "Cap reached" : ratio >= 0.8 ? "Near cap" : "Within cap"}`;
  m.exact.textContent = `${known ? value.toLocaleString("en") : "Usage unavailable"} / ${capped ? cap.toLocaleString("en") : "Cap unavailable"} tokens. Configured cap${day ? `. Ledger day: ${day}; server timezone: ${zone}` : ""}.`;
  return ratio !== null && ratio >= 1;
}
let snapshot = null,
  lastStatus = null,
  connected = false,
  streamConnected = false,
  initialized = false,
  currentRun = "",
  active = "queue",
  lastAnchor = "queue",
  terminal = null,
  errorEvent = null,
  visibleEvents = 5,
  controlBusy = false;
const events = new Map(),
  fileOrder = new Map(),
  boundaries = new Map(),
  done = new Map(),
  invalidatedEnds = new Set();
const invalidatedErrors = new Set();
let motion = null,
  poseTimer = null,
  announcementTimer = null,
  pendingAnnouncement = "",
  lastAnnounced = "";
const workerGraphic = graphic("worker-idle-0", "character", 40, 48);
$("worker").append(workerGraphic);
function pose(state = "idle", frame = 0) {
  workerGraphic
    .querySelector("use")
    .setAttribute("href", `#worker-${state}-${frame}`);
}
function cancelMotion() {
  if (motion) {
    motion.cancel();
    motion = null;
  }
  clearInterval(poseTimer);
  poseTimer = null;
  pose(
    lastStatus?.state === "running" && !active.startsWith("outcome:")
      ? stageInfo(lastStatus)?.[2] || "idle"
      : "idle",
    0,
  );
  if (!motionAllowed())
    for (const animation of document.getAnimations()) animation.cancel();
}
function motionAllowed() {
  return (
    connected &&
    streamConnected &&
    !snapshot?.statusError &&
    !snapshot?.logError &&
    !!snapshot?.status &&
    !document.hidden &&
    !reduced.matches &&
    $("motion").value !== "off" &&
    snapshot?.stopped === false &&
    lastStatus?.state === "running" &&
    fresh(lastStatus).ok &&
    !errorEvent &&
    !!stageInfo(lastStatus)
  );
}
function stageInfo(st) {
  if (!st || typeof st.stage !== "string") return null;
  if (/^implement:\d+$/.test(st.stage))
    return ["implement", "Implementing", "work"];
  return Object.hasOwn(stageMap, st.stage) ? stageMap[st.stage] : null;
}
function fresh(st) {
  const time = Date.parse(st?.updatedAt),
    age = Date.now() - time;
  if (!Number.isFinite(time) || age < 0)
    return { ok: false, text: "Update time unavailable" };
  return {
    ok: age <= 60000,
    text:
      age > 60000
        ? `No status update for ${Math.floor(age / 1000)}s`
        : `Status updated ${Math.floor(age / 1000)}s ago`,
  };
}
function anchor(id) {
  const parent = $("stations").getBoundingClientRect();
  const scale = parent.width / $("stations").offsetWidth || 1;
  if (id.startsWith("outcome:")) {
    const target = shelves.get(id.slice(8))?.section.getBoundingClientRect();
    if (target)
      return {
        left: (target.left - parent.left) / scale + 32,
        top: (target.top - parent.top) / scale + 48,
      };
  }
  const station = stations.get(id) || stations.get("queue"),
    box = station.section.getBoundingClientRect();
  if (innerWidth >= 1100)
    return {
      left: ((station.x / 1344) * parent.width) / scale,
      top: station.y,
    };
  return {
    left: (box.right - parent.left) / scale - 28,
    top: (box.top - parent.top) / scale + 84,
  };
}
function placeWorker() {
  const p = anchor(active);
  $("worker").style.left = `min(${p.left}px, calc(100% - 20px))`;
  $("worker").style.top = `${p.top}px`;
}
function cycle(kind) {
  if (!motionAllowed()) return;
  const beginning = performance.now();
  pose(kind, 0);
  clearInterval(poseTimer);
  poseTimer = setInterval(() => {
    const elapsed = performance.now() - beginning;
    if (elapsed >= 480 || !motionAllowed()) {
      clearInterval(poseTimer);
      poseTimer = null;
      pose(kind === "success" ? "idle" : kind, 0);
      return;
    }
    pose(kind, Math.floor(elapsed / 240) % 2);
  }, 80);
}
function moveWorker(from, to, kind, animate) {
  cancelMotion();
  placeWorker();
  if (!animate || !motionAllowed()) return;
  const source = anchor(from),
    target = anchor(to),
    mobile = innerWidth < 1100;
  if (mobile) {
    motion = $("worker").animate([{ opacity: 0 }, { opacity: 1 }], {
      duration: 160,
    });
  } else {
    const route = [];
    const point = (left, top) => ({ left: `${left}px`, top: `${top}px` });
    route.push(point(source.left, source.top));
    if (source.top === target.top) {
      route.push(point(target.left, target.top));
    } else if (from === "queue" && to === "claim") {
      const aisleX = (224 / 1344) * $("stations").offsetWidth;
      route.push(
        point(aisleX, source.top),
        point(aisleX, target.top),
        point(target.left, target.top),
      );
    } else {
      const aisleX = (1336 / 1344) * $("stations").offsetWidth;
      route.push(
        point(aisleX, source.top),
        point(aisleX, target.top),
        point(target.left, target.top),
      );
    }
    motion = $("worker").animate(route, { duration: 700, easing: "linear" });
    let f = 0;
    pose("walk", 0);
    poseTimer = setInterval(() => pose("walk", ++f % 4), 150);
  }
  const current = motion;
  current.onfinish = () => {
    if (motion !== current) return;
    motion = null;
    clearInterval(poseTimer);
    poseTimer = null;
    placeWorker();
    cycle(kind);
  };
}
function roundLabel(st) {
  if (!st || st.state === "idle") return "Round unavailable";
  const match = /^implement:(\d+)$/.exec(st.stage);
  const explicit = match ? Number(match[1]) : null,
    n = explicit ?? st.round;
  if (explicit !== null && st.round != null && explicit !== st.round)
    return "Round data mismatch";
  if (n == null) return "Round unavailable";
  return Number.isInteger(n) && n >= 1 && n <= 3
    ? `Round ${n} of 3`
    : "Round out of range";
}
function setBadge(item, text, tone = "") {
  item.result = text;
  item.badge.textContent = `${text.startsWith("Working") ? "●" : text.startsWith("Pass") ? "✓" : text.startsWith("Fail") ? "×" : text.startsWith("Done") ? "✓" : text.startsWith("Last reported") ? "Ⅱ" : "?"} ${text}`;
  item.badge.className = `badge ${tone}`;
}
function announce(text) {
  pendingAnnouncement = text;
  if (announcementTimer) return;
  announcementTimer = setTimeout(() => {
    announcementTimer = null;
    if (pendingAnnouncement !== lastAnnounced) {
      $("announcements").textContent = pendingAnnouncement;
      lastAnnounced = pendingAnnouncement;
    }
  }, 2000);
}
function renderState(s, fromNetwork = true) {
  const prior = lastStatus,
    previousRun = currentRun,
    previousActive = active;
  snapshot = s;
  connected = fromNetwork;
  if (
    prior &&
    s.status &&
    prior.run === s.status.run &&
    errorEvent &&
    (prior.stage !== s.status.stage || prior.updatedAt !== s.status.updatedAt)
  ) {
    const eventTime = Date.parse(errorEvent.ts),
      updateTime = Date.parse(s.status.updatedAt);
    if (!Number.isFinite(eventTime) || updateTime > eventTime)
      invalidatedErrors.add(errorEvent.id);
  }
  if (
    prior &&
    s.status &&
    prior.run === s.status.run &&
    prior.stage !== s.status.stage &&
    terminal
  ) {
    invalidatedEnds.add(terminal.eventId);
    terminal = null;
  }
  if (s.status) {
    lastStatus = s.status;
    currentRun = s.status.run || s.logIdentity || "";
    if (previousRun !== currentRun) {
      done.clear();
      terminal = null;
      errorEvent = null;
      lastAnchor = "queue";
    }
  }
  for (const row of s.events || []) addEvent(row, true);
  // Idle: the latest finished run is offered as static history only.
  if (!s.logAvailable)
    for (const row of s.lastEvents || []) addEvent({ ...row, lastRun: true }, true);
  for (const o of s.todayOutcomes || []) addOutcome(o);
  done.clear();
  terminal = null;
  errorEvent = null;
  for (const row of [...events.values()].sort(eventOrder)) enrich(row);
  const st = lastStatus,
    valid = st && ["running", "idle", "stopped", "capped"].includes(st.state);
  let connection = !connected
    ? "Connection lost · Last known state"
    : s.statusError
      ? lastStatus
        ? `${s.statusError} · Last known state`
        : s.statusError
      : !valid
        ? "Status unavailable"
        : !streamConnected
          ? "Connection lost · Last known state"
          : "";
  $("connection").textContent = connection;
  $("connection").hidden = !connection;
  if (connection) cancelMotion();
  $("test-label").hidden = !s.testData;
  const info = st?.state === "running" ? stageInfo(st) : null;
  const sameRunTerminal =
    terminal && terminal.card === st?.card && st?.state !== "idle";
  active =
    st?.state === "idle"
      ? "queue"
      : sameRunTerminal
        ? `outcome:${terminal.kind}`
        : info
          ? info[0]
          : lastAnchor;
  if (info) lastAnchor = info[0];
  $("stage").textContent =
    connection && !st
      ? "Waiting for status"
      : st?.state === "idle"
        ? "Idle"
        : sameRunTerminal
          ? {
              "ready-for-review": "Draft PR ready",
              "needs-clarification": "Questions asked",
              blocked: "Blocked",
            }[terminal.kind]
          : info
            ? info[1]
            : st?.state === "running"
              ? "Stage unavailable"
              : valid
                ? st.state === "capped"
                  ? "Capped"
                  : "Stopped"
                : "Unknown";
  $("state-label").textContent =
    `${st?.state === "running" ? "●" : "–"} ${st?.state === "running" ? "Running" : st?.state === "idle" ? "Idle" : valid ? st.state : "Unknown"}`;
  $("state-label").className =
    `badge ${st?.state === "running" ? "working" : ""}`;
  const hasCard = st?.state === "running" && !!st.card;
  $("card").hidden = !hasCard;
  $("no-card").hidden = hasCard;
  $("no-card").textContent =
    st?.state === "idle" ? "No active card" : "Card unavailable";
  $("card-key").textContent = st?.card || "";
  $("card-title").textContent = st?.title || "Title unavailable";
  $("full-title").textContent = st?.title || "Title unavailable";
  requestAnimationFrame(() => {
    $("title-disclosure").hidden =
      !$("title-disclosure").open &&
      $("card-title").scrollHeight <= $("card-title").clientHeight + 1;
  });
  $("worker").classList.toggle("carrying", hasCard);
  $("round").textContent = roundLabel(st);
  $("reported-tokens").textContent =
    `Reported tokens: ${validNumber(st?.tokens) ? st.tokens.toLocaleString("en") : "Unavailable"}`;
  const dailyCapped = budget(
      "daily-meter",
      s.today,
      s.dailyCap,
      s.usageDay,
      s.timezone,
    ),
    cardCapped = budget("card-meter", hasCard ? s.cardTokens : null, s.cardCap);
  const stopped = s.stopped === true,
    stopKnown = typeof s.stopped === "boolean";
  $("stop-label").textContent = stopped
    ? "Stopped · STOP set"
    : !stopKnown
      ? "Stop state unavailable"
      : st?.state === "running"
        ? "Running · STOP clear"
        : st?.state === "idle"
          ? "Ready · STOP clear"
          : "STOP clear";
  $("stop-label").className = stopped ? "banner danger" : "";
  const control = $("control");
  control.className = stopped ? "resume" : "";
  control.textContent = controlBusy
    ? stopped
      ? "Clearing STOP…"
      : "Setting STOP…"
    : !s.controls || !stopKnown
      ? "Control unavailable"
      : stopped
        ? "Resume autopilot"
        : "Stop autopilot";
  control.disabled = controlBusy || !s.controls || !stopKnown;
  $("control-reason").textContent = !s.controls
    ? "Read-only mode. STOP controls are disabled."
    : !stopKnown
      ? "Stop state unavailable. Controls are disabled."
      : "";
  const execution =
    st?.state === "running" && (stopped || dailyCapped || cardCapped);
  $("execution").hidden = !execution;
  $("execution").textContent = "Execution still reported running";
  $("error").hidden = !errorEvent;
  $("error").textContent = errorEvent
    ? "Error reported: " + errorEvent.message
    : "";
  $("note").hidden = !st?.note;
  $("note").textContent = st?.note || "";
  $("updated").textContent = fresh(st).text;
  $("status-details").textContent = JSON.stringify(
    {
      status: st,
      usageError: s.usageError,
      configError: s.configError,
      stopError: s.stopError,
      updatedAt: st?.updatedAt,
    },
    null,
    2,
  );
  // Only explicit status stages can be active. Guardrails have no structured
  // live stage telemetry, so they remain Unknown instead of simulating work.
  const running = st?.state === "running" && !connection;
  const curFacts = running ? deskFacts(currentRows()) : new Map();
  for (const [id, item] of stations) {
    if (id === "queue") continue;
    const isWorking = id === active;
    const finishedStage = isWorking && curFacts.get(id)?.doneSeen;
    item.section.classList.toggle("active", running && isWorking && !finishedStage);
    const result = done.get(id);
    const completedCurrent =
      result &&
      result.stage === st?.stage &&
      (!Number.isFinite(Date.parse(result.ts)) ||
        !Number.isFinite(Date.parse(st?.updatedAt)) ||
        Date.parse(result.ts) >= Date.parse(st.updatedAt));
    if (isWorking && stopped)
      setBadge(item, `Last reported: ${st.stage}`);
    else if (result && (completedCurrent || finishedStage))
      setBadge(item, result.text, result.tone);
    else if (id === active) setBadge(item, "Working", "working");
    else if (result) setBadge(item, result.text, result.tone);
    else
      setBadge(
        item,
        ["checks", "scope", "sensitive", "diff"].includes(id)
          ? `${item.section.getAttribute("aria-label")} · Unknown`
          : "Unknown",
      );
  }
  for (const [id, item] of stations) {
    const f = curFacts.get(id);
    item.section.classList.toggle("long-badge", item.result.startsWith("Last reported"));
    item.facts.hidden = !f;
    if (!f) continue;
    // Separate parts: only the rejection count carries the danger color, and
    // narrow desktop desks can show a shorter form (see style.css).
    const parts = [
      el("span", `✓ ${f.allowed}`, "facts-actions"),
      el("span", " · ", "facts-actions"),
      el("span", `× ${f.rejected}`, `facts-actions${f.rejected ? " facts-rejected" : ""}`),
    ];
    if (f.billable !== null)
      parts.push(el("span", " · ", "facts-actions"), el("span", `${compact(f.billable)} tokens`, "facts-tokens"));
    item.facts.replaceChildren(...parts);
    item.facts.setAttribute(
      "aria-label",
      `${f.allowed} actions allowed, ${f.rejected} rejected${f.billable !== null ? `, ${f.billable.toLocaleString("en")} billable tokens` : ""}`,
    );
    item.facts.className = `facts${f.billable !== null ? " has-tokens" : ""}`;
  }
  const [stateIcon, stateText, stateTone] = officeState(s, st, info, curFacts, connection, stopped, sameRunTerminal);
  $("office-state").textContent = `${stateIcon} ${stateText}`;
  $("office-state").className = `badge ${stateTone}`;
  const canInspect = running && stations.has(active);
  $("inspect-current").hidden = !canInspect;
  if (canInspect)
    $("inspect-current").textContent = `Inspect ${roleOf[active]}`;
  const tag = $("worker-tag");
  // At an outcome shelf the strip states the result; no stale role label.
  const atShelf = active.startsWith("outcome:");
  tag.hidden = !(running && hasCard && info) || atShelf;
  tag.textContent =
    running && hasCard && info
      ? `${curFacts.get(info[0])?.doneSeen ? "Last reported" : roleOf[info[0]]} · ${st.card}`
      : "";
  const outcomesKnown = Array.isArray(s.todayOutcomes);
  for (const shelf of shelves.values())
    shelf.subtitle.textContent = outcomesKnown
      ? `Today, from run logs${s.outcomesError ? ` · ${s.outcomesError}` : ""}`
      : "This session";
  $("activity-status").hidden = !!s.logAvailable && events.size > 0;
  $("activity-status").textContent = s.logAvailable
    ? "No events recorded."
    : s.lastRun && (s.lastEvents || []).length
      ? `Last run · ${s.lastRun.card} · ${s.lastRun.run} · finished run history, not live`
      : s.lastRunError || "Activity unavailable";
  $("boards").textContent = s.boards?.length
    ? `Configured boards: ${s.boards.join(", ")} (configuration source; queue unavailable)`
    : "Configured boards unavailable";
  const runSignature = JSON.stringify(s.runs);
  if ($("recent-runs").dataset.signature !== runSignature) {
    $("recent-runs").dataset.signature = runSignature;
    $("recent-runs").replaceChildren(
      ...(s.runs || []).map((run) =>
        el("li", `${run.card} · ${run.run} · ${run.modified}`),
      ),
    );
  }
  $("runs-error").textContent = s.runsError || "";
  const sameRun = prior && prior.run && prior.run === st?.run,
    stageChanged = sameRun && prior.stage !== st.stage;
  const route = [
    "queue",
    "claim",
    "setup",
    "preflight",
    "implement",
    "checks",
    "scope",
    "sensitive",
    "diff",
    "publish",
  ];
  const adjacent =
    Math.abs(route.indexOf(previousActive) - route.indexOf(active)) === 1 ||
    (info?.[0] === "implement" && /^implement:[23]$/.test(st.stage));
  if (previousActive !== active || stageChanged || previousRun !== currentRun) {
    moveWorker(
      previousActive,
      active,
      info?.[2] || "idle",
      initialized && stageChanged && adjacent && !connection,
    );
  } else if (!motionAllowed()) cancelMotion();
  else if (!motion) placeWorker();
  if (initialized && stageChanged) announce($("stage").textContent);
  initialized = true;
}
$("title-disclosure").addEventListener("toggle", () => {
  $("card").classList.toggle("title-open", $("title-disclosure").open);
});

function eventContext(row) {
  const n = row.native,
    file = row.id.split(":")[0];
  if (n.kind === "start" && typeof n.card === "string" && n.card)
    boundaries.set(file, n.card);
  if (
    typeof n.run === "string" &&
    n.run &&
    typeof n.card === "string" &&
    n.card
  )
    return { run: n.run, card: n.card };
  if (boundaries.get(file) && n.card === boundaries.get(file))
    return { run: `boundary:${file}`, card: n.card };
  return null;
}
function eventOrder(a, b) {
  const [fileA, offsetA] = a.id.split(":"),
    [fileB, offsetB] = b.id.split(":");
  return fileA === fileB
    ? Number(offsetA) - Number(offsetB)
    : fileOrder.get(fileA) - fileOrder.get(fileB);
}
function isCurrent(row, context) {
  return (
    !!context &&
    !!lastStatus &&
    context.card === lastStatus.card &&
    (lastStatus.run
      ? context.run === lastStatus.run
      : row.id.split(":")[0] === snapshot?.logIdentity &&
        boundaries.get(snapshot.logIdentity) === lastStatus.card)
  );
}
function enrich(row) {
  const n = row.native,
    context = eventContext(row),
    same = isCurrent(row, context);
  if (!same) return;
  if (n.kind === "stage-done") {
    const target = stageInfo({ stage: n.stage })?.[0];
    if (target)
      done.set(target, {
        text: "Done",
        tone: "",
        stage: n.stage,
        ts: n.ts,
      });
  }
  if (
    n.kind === "error" &&
    !invalidatedErrors.has(row.id) &&
    !(
      Number.isFinite(Date.parse(n.ts)) &&
      Date.parse(lastStatus.updatedAt) > Date.parse(n.ts)
    )
  )
    errorEvent = {
      id: row.id,
      message: typeof n.msg === "string" ? n.msg : "Unavailable",
      ts: n.ts,
    };
  if (n.kind === "stage" && stageInfo({ stage: n.stage })) {
    errorEvent = null;
    terminal = null;
  }
  if (
    n.kind === "end" &&
    shelves.has(n.data?.kind) &&
    !invalidatedEnds.has(row.id)
  ) {
    const endTime = Date.parse(n.ts),
      statusTime = Date.parse(lastStatus.updatedAt);
    if (
      Number.isFinite(endTime) &&
      Number.isFinite(statusTime) &&
      statusTime > endTime
    )
      return;
    terminal = { ...context, kind: n.data.kind, eventId: row.id };
  }
}
function eventMessage(n) {
  const action =
    typeof n.data?.action === "string" ? n.data.action : "Unavailable";
  switch (n.kind) {
    case "start":
      return ["–", `Run started${n.card ? " · " + n.card : ""}`, ""];
    case "stage":
      return ["●", `Stage: ${n.stage || "Unavailable"}`, ""];
    case "stage-done":
      return ["✓", `Stage done: ${n.stage || "Unavailable"}`, ""];
    case "permission":
      return n.data?.allow === true
        ? ["✓", `Allowed: ${action}`, "event-success"]
        : n.data?.allow === false
          ? ["×", `Rejected: ${action}`, "event-danger"]
          : ["?", `Permission result unavailable: ${action}`, ""];
    case "tokens":
      return ["–", "Token usage updated", ""];
    case "verdict":
      return ["?", "Verdict received · Details unavailable", ""];
    case "error":
      return ["×", `Error: ${n.msg || "Unavailable"}`, "event-danger"];
    case "end":
      return n.data?.kind === "ready-for-review"
        ? ["✓", "Draft PR ready", "event-success"]
        : n.data?.kind === "needs-clarification"
          ? ["?", "Questions asked", ""]
          : n.data?.kind === "blocked"
            ? ["Ⅱ", "Blocked", ""]
            : ["?", "Unrecognized event", ""];
    default:
      return ["?", "Unrecognized event", ""];
  }
}
// One line that says what the office is doing, by precedence. Every branch
// comes from status.json, STOP, or attributable run-log records.
function officeState(s, st, info, curFacts, connection, stopped, sameRunTerminal) {
  if (connection) return ["?", connection, "warning"];
  if (stopped)
    return [
      "Ⅱ",
      `Stopped · STOP set${st?.state === "running" ? " · execution still reported running" : ""}`,
      "danger",
    ];
  if (st?.state === "running") {
    if (errorEvent)
      return ["×", `Error reported · during ${st.stage || "unknown stage"}`, "danger"];
    if (sameRunTerminal)
      return {
        "ready-for-review": ["✓", `Ready for review · ${st.card || "card unavailable"}`, "success"],
        "needs-clarification": ["?", `Questions asked · ${st.card || "card unavailable"}`, "warning"],
        blocked: ["Ⅱ", `On hold · ${st.card || "card unavailable"}`, "warning"],
      }[terminal.kind];
    const freshness = fresh(st);
    if (!freshness.ok)
      return ["–", `${freshness.text} · last reported ${st.stage || "stage unavailable"}`, "warning"];
    if (!info) return ["?", "Running · Stage unavailable", ""];
    const f = curFacts.get(info[0]);
    if (f && f.doneSeen)
      return ["–", `Between reported stages · ${st.stage} finished, next step not logged`, ""];
    return ["●", `Working · ${info[1]} · ${roleOf[info[0]]}`, "working"];
  }
  if (st?.state === "idle") {
    const lastEnd = lastRows().filter((r) => r.native.kind === "end").pop();
    const kind = lastEnd?.native.data?.kind;
    const label = {
      "ready-for-review": ["Draft PR ready", "success"],
      "needs-clarification": ["Questions asked", "warning"],
      blocked: ["On hold", "warning"],
    }[kind];
    if (label && snapshot?.lastRun)
      return ["–", `Idle · last run ${snapshot.lastRun.card}: ${label[0]}`, label[1]];
    return ["–", "Idle", ""];
  }
  return ["?", st ? "Status unavailable" : "Waiting for status", "warning"];
}
// Real `end` records from today's run logs, deduplicated with live events.
function addOutcome(o) {
  const shelf = shelves.get(o?.kind);
  if (!shelf || typeof o.card !== "string" || typeof o.run !== "string") return;
  const key = `${o.run}:${o.card}`;
  if (shelf.items.has(key)) return;
  const rounds = validNumber(o.rounds) ? ` · ${o.rounds} coding round${o.rounds === 1 ? "" : "s"}` : "";
  shelf.items.set(key, {
    key,
    card: o.card,
    title: "",
    pr: typeof o.pr === "string" ? o.pr : "",
    reason: `${textOr(o.summary, "Summary unavailable")}${rounds}${o.ts ? ` · ${o.ts}` : ""}`,
  });
  renderShelf(shelf);
}
let inspectorReturn = null;
const dlRow = (dl, label, value) => {
  dl.append(el("dt", label), el("dd", value));
};
function openInspector(id, trigger) {
  const item = stations.get(id);
  if (!item) return;
  inspectorReturn = trigger || null;
  const running = lastStatus?.state === "running";
  const cur = running ? deskFacts(currentRows()) : new Map();
  const last = !running ? deskFacts(lastRows()) : new Map();
  const f = cur.get(id) || last.get(id) || null;
  const scope = cur.get(id)
    ? `Current run · ${lastStatus.card || "card unavailable"}`
    : last.get(id)
      ? `Last run · ${snapshot.lastRun.card} · finished history, not live`
      : running
        ? `Current run · ${lastStatus.card || "card unavailable"} · no log records for this desk yet`
        : "No run data for this desk";
  const name = item.section.getAttribute("aria-label");
  $("inspector-heading").textContent = `${name} · ${roleOf[id]}`;
  const body = $("inspector-body");
  body.replaceChildren();
  const dl = el("dl", null, "inspector-facts");
  dlRow(dl, "Desk status", item.result);
  dlRow(dl, "Scope", scope);
  dlRow(
    dl,
    "Handled by",
    id === "queue"
      ? "Not a worker desk"
      : `${roleOf[id]} · ${modelRole.has(id) ? "separate AI agent session" : "engine step, no model"}`,
  );
  if (f) {
    dlRow(dl, "Session", f.session || "Unavailable · no stage event for this desk in this run");
    dlRow(dl, "Started", f.started || "Unavailable");
    dlRow(
      dl,
      "Finished",
      f.doneSeen ? f.done || "Finished · time unavailable" : "Not finished · no stage-done event yet",
    );
    dlRow(
      dl,
      "Billable tokens",
      f.billable !== null
        ? f.billable.toLocaleString("en")
        : f.doneSeen
          ? "Unavailable · stage-done carried no billable count"
          : "Unavailable · reported when the stage finishes",
    );
    dlRow(dl, "Actions", `${f.allowed} allowed · ${f.rejected} rejected`);
  }
  body.append(dl);
  if (telemetryGap[id]) body.append(el("p", telemetryGap[id], "meta"));
  if (f && f.perms.length) {
    body.append(el("h3", `Recent actions (last ${f.perms.length})`));
    const list = el("ol", null, "inspector-actions");
    for (const p of [...f.perms].reverse()) {
      const li = el("li");
      const mark = p.allow === true ? "✓ Allowed" : p.allow === false ? "× Rejected" : "? Unknown";
      li.append(
        el("span", `${mark}: ${p.action}`, `event-${p.allow === true ? "success" : p.allow === false ? "danger" : "muted"}`),
      );
      if (p.reason) li.append(el("p", p.reason, "meta"));
      if (p.ts) {
        const t = el("time", p.ts);
        t.dateTime = p.ts;
        li.append(t);
      }
      list.append(li);
    }
    body.append(list);
  }
  if (f && f.summary) {
    const d = el("details");
    d.append(el("summary", "Agent's own summary · not verified"), el("pre", f.summary));
    body.append(d);
  }
  $("inspector").showModal();
  $("inspector-close").focus();
}
$("inspector-close").addEventListener("click", () => $("inspector").close());
$("inspector").addEventListener("close", () => inspectorReturn?.focus());
$("inspector").addEventListener("keydown", (e) => {
  if (e.key !== "Tab") return;
  const focusable = [...$("inspector").querySelectorAll("button, summary, a[href]")];
  const first = focusable[0],
    lastEl = focusable[focusable.length - 1];
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    lastEl.focus();
  } else if (!e.shiftKey && document.activeElement === lastEl) {
    e.preventDefault();
    first.focus();
  }
});
$("inspect-current").addEventListener("click", () =>
  openInspector(active, $("inspect-current")),
);
function addEvent(row, history = false) {
  if (!row || typeof row.id !== "string" || !row.native || events.has(row.id))
    return;
  const n = row.native,
    context = eventContext(row),
    same = isCurrent(row, context);
  events.set(row.id, row);
  const file = row.id.split(":")[0];
  if (!fileOrder.has(file)) fileOrder.set(file, fileOrder.size);
  if (context && !row.lastRun && n.kind === "end" && shelves.has(n.data?.kind)) {
    const kind = n.data.kind,
      shelf = shelves.get(kind),
      key = `${context.run}:${context.card}`;
    if (!shelf.items.has(key)) {
      shelf.items.set(key, {
        key,
        card: context.card,
        title:
          typeof n.data?.title === "string"
            ? n.data.title
            : same
              ? lastStatus.title
              : "",
        pr: typeof n.data?.pr === "string" ? n.data.pr : "",
        reason: typeof n.msg === "string" ? n.msg : "",
      });
      renderShelf(shelf);
    }
    if (same) terminal = { ...context, kind, eventId: row.id };
    if (!history) announce(eventMessage(n)[1]);
  }
  const [icon, message, tone] = eventMessage(n),
    li = el("li");
  li.dataset.eventId = row.id;
  li.append(el("span", icon, `event-icon ${tone}`));
  const body = el("div"),
    msg = el("p", message, "message");
  body.append(msg);
  if (typeof n.ts === "string" && n.ts) {
    const stamp = el("time", n.ts);
    stamp.dateTime = n.ts;
    body.append(stamp);
  }
  const details = el("details"),
    summary = el("summary", "Details"),
    pre = el("pre", JSON.stringify(n, null, 2));
  details.append(summary, pre);
  body.append(details);
  li.append(body);
  const before = [...$("events").children].find(
    (node) => eventOrder(row, events.get(node.dataset.eventId)) > 0,
  );
  $("events").insertBefore(li, before || null);
  if (!history && motionAllowed())
    li.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 160 });
  while (events.size > 1000) {
    const oldest = $("events").lastElementChild.dataset.eventId;
    events.delete(oldest);
    $("events").lastElementChild?.remove();
  }
  renderEventVisibility();
  if (!history && ["stage", "verdict"].includes(n.kind)) announce(message);
}
function renderEventVisibility() {
  [...$("events").children].forEach(
    (node, i) => (node.hidden = i >= visibleEvents),
  );
  $("older").hidden = events.size <= visibleEvents;
}
$("older").addEventListener("click", () => {
  visibleEvents += 20;
  renderEventVisibility();
});
function validPR(raw) {
  try {
    const u = new URL(raw);
    return ["http:", "https:"].includes(u.protocol) &&
      !u.username &&
      !u.password
      ? u.href
      : null;
  } catch {
    return null;
  }
}
function renderShelf(shelf) {
  shelf.empty.hidden = shelf.items.size > 0;
  shelf.icon
    .querySelector("use")
    .setAttribute("href", `#prop-shelf-${shelf.items.size ? "full" : "empty"}`);
  const existing = new Map(
    [...shelf.list.children].map((n) => [n.dataset.key, n]),
  );
  const values = [...shelf.items.values()].reverse();
  for (const [i, value] of values.entries()) {
    let node = existing.get(value.key);
    if (!node) {
      node = el("article", null, "card-chip");
      node.dataset.key = value.key;
      node.append(
        el("p", value.card, "meta"),
        el(
          "span",
          {
            "ready-for-review": "✓ Draft PR ready",
            "needs-clarification": "? Questions asked",
            blocked: "Ⅱ Blocked",
          }[shelf.kind],
          `badge ${shelf.tone}`,
        ),
        el("p", value.title || "Title unavailable", "clamped"),
      );
      const disclosure = el("details"),
        summary = el("summary", "Show full title");
      disclosure.append(summary, el("p", value.title || "Title unavailable"));
      disclosure.addEventListener("toggle", () =>
        node.classList.toggle("title-open", disclosure.open),
      );
      node.append(disclosure);
      const pr = validPR(value.pr);
      if (pr) {
        const link = el("a", "Open draft PR");
        link.href = pr;
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        node.append(link);
      }
      if (value.reason) {
        const d = el("details");
        d.append(el("summary", "Outcome details"), el("pre", value.reason));
        node.append(d);
      }
      shelf.list.append(node);
    }
    node.hidden = !shelf.expanded && i >= 3;
    shelf.list.append(node);
  }
  shelf.more.hidden = shelf.expanded || values.length <= 3;
  shelf.more.textContent = `Show all (${values.length})`;
}

let pendingAction = null,
  controlDeadline = null;
$("control").addEventListener("click", () => {
  if (controlBusy || typeof snapshot?.stopped !== "boolean") return;
  pendingAction = snapshot.stopped ? "resume" : "stop";
  $("confirm-heading").textContent =
    pendingAction === "stop" ? "Stop autopilot?" : "Resume autopilot?";
  $("confirm-body").textContent =
    pendingAction === "stop"
      ? "Set STOP. Work may continue until the autopilot checks it."
      : "Clear STOP. The autopilot may continue work.";
  $("confirm-action").textContent =
    pendingAction === "stop" ? "Set STOP" : "Clear STOP";
  $("confirm").showModal();
  $("cancel").focus();
});
$("cancel").addEventListener("click", () => $("confirm").close());
$("confirm").addEventListener("close", () => $("control").focus());
$("confirm").addEventListener("keydown", (e) => {
  if (e.key !== "Tab") return;
  const first = $("cancel"),
    last = $("confirm-action");
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
});
$("confirm-action").addEventListener("click", async () => {
  if (controlBusy || !pendingAction) return;
  const action = pendingAction;
  pendingAction = null;
  $("confirm").close();
  controlBusy = true;
  $("control-feedback").textContent =
    action === "stop" ? "Setting STOP…" : "Clearing STOP…";
  renderState(snapshot, connected);
  clearTimeout(controlDeadline);
  controlDeadline = setTimeout(() => {
    $("control-feedback").textContent =
      "Change not confirmed. Check STOP state.";
    controlBusy = false;
    renderState(snapshot, connected);
  }, 10000);
  try {
    const response = await fetch(`/api/${action}`, {
      method: "POST",
      headers: {
        "X-Office-Token": document.querySelector('meta[name="office-token"]')
          .content,
      },
    });
    if (!response.ok) throw Error("control");
    const readback = await response.json();
    renderState(readback, true);
    if (readback.stopped === (action === "stop")) {
      $("control-feedback").textContent = readback.stopped
        ? "Stopped · STOP set"
        : "STOP clear";
      clearTimeout(controlDeadline);
    } else throw Error("readback");
  } catch {
    $("control-feedback").textContent = "Could not change STOP. Try again.";
    clearTimeout(controlDeadline);
  } finally {
    controlBusy = false;
    renderState(snapshot, connected);
    $("control").focus();
  }
});

async function refresh() {
  try {
    const response = await fetch("/api/state");
    if (!response.ok) throw Error("state");
    const s = await response.json();
    renderState(s, true);
  } catch {
    connected = false;
    cancelMotion();
    if (snapshot) renderState(snapshot, false);
    else {
      $("connection").textContent = "Waiting for status";
      $("connection").hidden = false;
    }
  }
}
const source = new EventSource("/api/events");
source.addEventListener("open", () => {
  streamConnected = true;
  if (snapshot) renderState(snapshot, connected);
});
source.addEventListener("log", (e) => {
  try {
    streamConnected = true;
    const row = JSON.parse(e.data),
      isNew = !events.has(row.id),
      wasInitialized = initialized;
    addEvent(row, row.history || !initialized);
    if (snapshot) renderState(snapshot, connected);
    if (
      isNew &&
      wasInitialized &&
      !row.history &&
      row.native.kind === "stage" &&
      isCurrent(row, eventContext(row)) &&
      row.native.stage === lastStatus?.stage
    )
      cycle(stageInfo(lastStatus)?.[2] || "idle");
  } catch {
    cancelMotion();
  }
});
source.addEventListener("telemetry-error", () => {
  streamConnected = false;
  cancelMotion();
  if (snapshot) renderState(snapshot, connected);
});
source.addEventListener("telemetry-ready", () => {
  streamConnected = true;
  if (snapshot) renderState(snapshot, connected);
});
source.addEventListener("error", () => {
  streamConnected = false;
  cancelMotion();
  if (snapshot) renderState(snapshot, connected);
});
document.addEventListener("visibilitychange", () => {
  cancelMotion();
  placeWorker();
  if (!document.hidden) refresh();
});
addEventListener("resize", () => {
  cancelMotion();
  placeWorker();
});
new ResizeObserver(() => {
  cancelMotion();
  placeWorker();
}).observe($("stations"));
refresh();
setInterval(refresh, 2000);
