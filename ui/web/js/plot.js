// The scope screens: one uPlot per port panel, drawn from the samples the server sent. Instances and
// rings live in this module's scope, never in the store state (uPlot would be wrapped, copied or
// compared by anything that watches the state; Research Pitfall 9). Samples are appended in the
// caller's message handler, so a hidden tab loses nothing; drawing happens in requestAnimationFrame,
// at most 20 times a second, and once when the tab becomes visible.
//
// The screen shows what the API reported and nothing else: the x range is the last 60 s of sim time
// ending at the newest sample, the RPM axis is 0..max_rpm of the plugged fan and the duty axis
// 0..100 (nothing autoscales), a null value is a gap, and no point is resampled, smoothed or
// interpolated. Each scope draws three traces: the shaft RPM (solid), the duty (dashed, stepped)
// and the tach RPM, what the firmware's tach counting reads, as a stepped dimmer line in the port
// colour on the RPM scale. A tach value above max_rpm (quantisation, ripple) is clipped by the plot
// area, never rescaled (Research Pitfall 7). This module derives no gap from sample timing.
import { Ring } from "./ring.js";

export const WINDOW_S = 60;
export const SAMPLE_S = 0.1;
export const RING_CAP = 620;
export const RING_SLACK = 64;

const DRAW_MIN_MS = 50;
const GRID = "#20252b";
const CENTER = "#2e343b";
const FALLBACK_COLOR = "#9aa1a9";
const AXIS_FONT = "12px system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif";

const WAITING_BODY = (port) =>
  "The simulator has not reported port " + port + " yet. The trace starts with the first sample; nothing to do.";
const CONNECTING_BODY = "Connecting to the simulator. This screen fills when the first snapshot arrives.";

// rings: port -> Ring. A ring exists as soon as a sample for the port arrives, whether or not the
// panel is on the page yet.
const rings = new Map();
// fanMax: port -> max_rpm of the plugged fan, or null while unknown.
const fanMax = new Map();
// scopes: panel element -> scope {panel, port, u, observer, maxRpm}.
const scopes = new Map();

let linkState = "connecting";
let scheduled = false;
let lastDraw = -Infinity;
let listening = false;

function ringOf(port) {
  let r = rings.get(port);
  if (r === undefined) {
    r = new Ring(RING_CAP, RING_SLACK);
    rings.set(port, r);
  }
  return r;
}

// ringFor is the ring of a port (created empty when there is none); tests read it.
export function ringFor(port) {
  return ringOf(port);
}

function scopeOfPort(port) {
  for (const sc of scopes.values()) {
    if (sc.port === port) {
      return sc;
    }
  }
  return null;
}

// scopeFor is the uPlot instance of a port's scope, or null before the panel exists. Tests read it.
export function scopeFor(port) {
  const sc = scopeOfPort(port);
  return sc === null ? null : sc.u;
}

function portOf(panel) {
  const n = Number(panel.dataset.port);
  return Number.isInteger(n) ? n : 0;
}

function channelColor(panel) {
  const v = getComputedStyle(panel).getPropertyValue("--ch").trim();
  return v === "" ? FALLBACK_COLOR : v;
}

// withAlpha is "#rrggbb" as rgba(r, g, b, a); any other text is returned unchanged.
function withAlpha(color, alpha) {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(color);
  if (m === null) {
    return color;
  }
  return "rgba(" + parseInt(m[1], 16) + ", " + parseInt(m[2], 16) + ", " + parseInt(m[3], 16) + ", " + alpha + ")";
}

// tmaxOf is the newest sample's sim time in seconds (the right edge of the screen).
function tmaxOf(ring) {
  return ring.xs.length === 0 ? 0 : ring.xs[ring.xs.length - 1];
}

// emptyBody is the dim line under the heading of an empty screen.
function emptyBody(port) {
  return linkState === "connecting" ? CONNECTING_BODY : WAITING_BODY(port);
}

// paintEmpty shows or hides the empty-state text of one scope: visible until the port's first
// non-null sample; the body says "connecting" while LINK is CONNECTING.
function paintEmpty(sc) {
  const box = sc.panel.querySelector(".scope .empty");
  if (box === null) {
    return;
  }
  const body = box.querySelector(".empty-body");
  const text = emptyBody(sc.port);
  if (body !== null && body.textContent !== text) {
    body.textContent = text;
  }
  box.hidden = ringOf(sc.port).seen;
}

function makeScope(panel, port) {
  const host = panel.querySelector(".scope .plot");
  const wrap = panel.querySelector(".scope");
  const sc = { panel, port, u: null, observer: null, maxRpm: fanMax.has(port) ? fanMax.get(port) : null };
  const ring = ringOf(port);
  const color = () => channelColor(panel);
  const dutyColor = () => withAlpha(color(), 0.7);
  const size = () => ({ width: Math.max(10, wrap.clientWidth - 2), height: Math.max(10, wrap.clientHeight - 2) });
  const known = () => typeof sc.maxRpm === "number" && sc.maxRpm > 0;
  const opts = {
    ...size(),
    padding: [6, 4, 6, 4],
    legend: { show: false },
    cursor: { show: false },
    scales: {
      x: { time: false, auto: false, range: () => [tmaxOf(ring) - WINDOW_S, tmaxOf(ring)] },
      rpm: { auto: false, range: () => [0, known() ? sc.maxRpm : 1] },
      duty: { auto: false, range: [0, 100] },
    },
    axes: [
      {
        scale: "x",
        size: 0,
        gap: 0,
        values: () => [],
        ticks: { show: false },
        border: { show: false },
        grid: { stroke: GRID, width: 1 },
        splits: (u, i, min, max) => Array.from({ length: 11 }, (_, k) => min + (k * (max - min)) / 10),
      },
      {
        scale: "rpm",
        size: 48,
        gap: 4,
        font: AXIS_FONT,
        stroke: color,
        ticks: { show: false },
        border: { show: false },
        grid: { stroke: GRID, width: 1 },
        splits: () => (known() ? [0, sc.maxRpm / 4, sc.maxRpm / 2, (sc.maxRpm * 3) / 4, sc.maxRpm] : []),
        values: (u, s) => s.map((v, i) => (i % 2 === 0 ? String(Math.round(v)) : "")),
      },
      {
        scale: "duty",
        side: 1,
        size: 36,
        gap: 4,
        font: AXIS_FONT,
        stroke: dutyColor,
        ticks: { show: false },
        border: { show: false },
        grid: { show: false },
        splits: () => [0, 25, 50, 75, 100],
        values: (u, s) => s.map((v, i) => (i % 2 === 0 ? String(v) : "")),
      },
    ],
    series: [
      {},
      { label: "RPM", scale: "rpm", show: known(), stroke: color, width: 2, spanGaps: false, points: { show: false } },
      {
        label: "DUTY",
        scale: "duty",
        stroke: color,
        width: 1,
        dash: [4, 4],
        spanGaps: false,
        points: { show: false },
        paths: globalThis.uPlot.paths.stepped({ align: 1 }),
      },
      {
        label: "TACH",
        scale: "rpm",
        show: known(),
        stroke: () => withAlpha(color(), 0.5),
        width: 1,
        spanGaps: false,
        points: { show: false },
        paths: globalThis.uPlot.paths.stepped({ align: 1 }),
      },
    ],
    hooks: {
      // The centre lines of the graticule, over the grid and under the traces.
      drawAxes: [
        (u) => {
          const { left, top, width, height } = u.bbox;
          const ctx = u.ctx;
          const lw = Math.max(1, Math.round(globalThis.devicePixelRatio || 1));
          ctx.save();
          ctx.strokeStyle = CENTER;
          ctx.lineWidth = lw;
          ctx.beginPath();
          const x = Math.round(left + width / 2) + 0.5;
          const y = Math.round(top + height / 2) + 0.5;
          ctx.moveTo(x, top);
          ctx.lineTo(x, top + height);
          ctx.moveTo(left, y);
          ctx.lineTo(left + width, y);
          ctx.stroke();
          ctx.restore();
        },
      ],
    },
  };
  sc.u = new globalThis.uPlot(opts, [ring.xs, ring.rpm, ring.duty, ring.tach], host);
  sc.observer = new ResizeObserver(() => {
    const s = size();
    if (sc.u.width !== s.width || sc.u.height !== s.height) {
      sc.u.setSize(s);
    }
  });
  sc.observer.observe(wrap);
  return sc;
}

function destroyScope(sc) {
  sc.observer.disconnect();
  sc.u.destroy();
}

// initScopes makes a uPlot for every panel of `panels` (an iterable of .port elements carrying
// data-port) that has none, and drops the scopes of panels that left the page. It is idempotent.
export function initScopes(panels) {
  ensureListeners();
  const live = new Set(panels);
  for (const [panel, sc] of scopes) {
    if (!live.has(panel)) {
      destroyScope(sc);
      scopes.delete(panel);
    }
  }
  for (const panel of live) {
    const port = portOf(panel);
    const have = scopes.get(panel);
    if (have === undefined) {
      const sc = makeScope(panel, port);
      scopes.set(panel, sc);
      paintEmpty(sc);
      sc.dirty = true;
    } else if (have.port !== port) {
      have.port = port;
      paintEmpty(have);
      have.dirty = true;
    }
  }
  if ([...scopes.values()].some((sc) => sc.dirty)) {
    schedule(false);
  }
}

// setLink tells the scopes the link state (connecting, live or retrying): the empty-state body of
// a screen without a sample says "connecting" while the page has no snapshot yet.
export function setLink(next) {
  if (next === linkState) {
    return;
  }
  linkState = next;
  for (const sc of scopes.values()) {
    paintEmpty(sc);
  }
}

// setFanMax fixes the RPM axis of a port at 0..max (the plugged fan's max_rpm); null when the fan
// is unknown, which hides the RPM and TACH traces and the labels rather than guess a scale. The scale is set
// only when the value changed.
export function setFanMax(port, max) {
  const next = typeof max === "number" && max > 0 ? max : null;
  if (fanMax.has(port) && fanMax.get(port) === next) {
    return;
  }
  fanMax.set(port, next);
  const sc = scopeOfPort(port);
  if (sc === null) {
    return;
  }
  const was = sc.maxRpm;
  sc.maxRpm = next;
  if (next === null) {
    sc.u.setSeries(1, { show: false });
    sc.u.setSeries(3, { show: false });
    return;
  }
  sc.u.setScale("rpm", { min: 0, max: next });
  if (was === null) {
    sc.u.setSeries(1, { show: true });
    sc.u.setSeries(3, { show: true });
  }
}

// markDirty asks for a redraw of every scope whose port has new points.
function markDirty(port) {
  const sc = scopeOfPort(port);
  if (sc !== null) {
    sc.dirty = true;
  }
}

function appendSample(frame) {
  const gap = frame.gap === true;
  for (const e of Array.isArray(frame.v) ? frame.v : []) {
    if (!Number.isInteger(e.port)) {
      continue;
    }
    const ring = ringOf(e.port);
    const was = ring.seen;
    ring.push(frame.t, e.rpm, e.duty, gap, e.tach);
    if (!was && ring.seen) {
      const sc = scopeOfPort(e.port);
      if (sc !== null) {
        paintEmpty(sc);
      }
    }
    markDirty(e.port);
  }
}

// pushSample adds one `sample` frame ({t, k, seq, v: [{port, duty, rpm, tach}], gap?}) to the rings. It
// is called from the WebSocket message handler.
export function pushSample(frame) {
  appendSample(frame);
  schedule(false);
}

// loadHistory replaces the rings with the samples of a GET /api/v1/history result (a reconnect
// never merges two epochs).
export function loadHistory(result) {
  resetScopes();
  for (const s of Array.isArray(result.samples) ? result.samples : []) {
    appendSample(s);
  }
  schedule(false);
}

// resetScopes forgets every point of every port; the screens show their empty state again.
export function resetScopes() {
  for (const ring of rings.values()) {
    ring.clear();
  }
  for (const sc of scopes.values()) {
    paintEmpty(sc);
    sc.dirty = true;
  }
  schedule(false);
}

function drawDirty() {
  for (const sc of scopes.values()) {
    if (sc.dirty) {
      sc.dirty = false;
      const ring = ringOf(sc.port);
      sc.u.setData([ring.xs, ring.rpm, ring.duty, ring.tach]);
    }
  }
}

// stale is true while the link is lost after having been live: the scopes keep their last picture
// and draw nothing until the link is back.
let stale = false;

// setStale freezes (true) or thaws (false) the scopes. Thawing redraws what is pending.
export function setStale(next) {
  if (next === stale) {
    return;
  }
  stale = next;
  if (!stale) {
    schedule(true);
  }
}

function onFrame(ts) {
  scheduled = false;
  if (stale) {
    return;
  }
  if (ts - lastDraw < DRAW_MIN_MS) {
    schedule(false);
    return;
  }
  lastDraw = ts;
  drawDirty();
}

function schedule(force) {
  if (force) {
    lastDraw = -Infinity;
  }
  if (scheduled) {
    return;
  }
  scheduled = true;
  requestAnimationFrame(onFrame);
}

function ensureListeners() {
  if (listening) {
    return;
  }
  listening = true;
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      for (const sc of scopes.values()) {
        sc.dirty = true;
      }
      schedule(true);
    }
  });
}
