// The readout renderer: turns the state mirror (store.js) and the link state into text on the page.
// It formats what the API reported and derives nothing. Every value element carries data-src (a
// dotted path into the status JSON) and data-fmt (the formatter); until the first snapshot, and
// for a path the state does not hold, the element shows `--`, never a fake zero.

const NONE = "--";

const LINK_TEXT = {
  connecting: "CONNECTING",
  live: "LIVE",
  retrying: "DISCONNECTED, RETRYING",
};

function pad(n, width) {
  return String(n).padStart(width, "0");
}

// simtime formats seconds of sim time as HH:MM:SS.s. The value is rounded to whole microseconds
// first and the tenth is truncated in integers, so float noise never moves a digit.
function simtime(seconds) {
  const tenths = Math.floor(Math.round(seconds * 1e6) / 1e5);
  const whole = Math.floor(tenths / 10);
  const h = Math.floor(whole / 3600);
  const m = Math.floor((whole % 3600) / 60);
  const s = whole % 60;
  return pad(h, 2) + ":" + pad(m, 2) + ":" + pad(s, 2) + "." + (tenths % 10);
}

const formats = {
  text: (v) => String(v),
  chip: (v) => "P" + v,
  rpm: (v) => String(Math.round(v)),
  pct: (v) => (v * 100).toFixed(1),
  simtime,
  factor: (v) => "x" + v,
  firmware: (v) => (v ? "CONNECTED" : "DISCONNECTED"),
};

// lookup walks a dotted path ("ports.0.rpm") through the state; undefined when any step is missing.
function lookup(state, path) {
  let cur = state;
  for (const key of path.split(".")) {
    if (cur === null || typeof cur !== "object") {
      return undefined;
    }
    cur = cur[key];
  }
  return cur;
}

// shown is the text for one value element.
function shown(state, el) {
  if (state === null) {
    return NONE;
  }
  const v = lookup(state, el.dataset.src);
  if (v === undefined || v === null) {
    return NONE;
  }
  const fmt = formats[el.dataset.fmt];
  return fmt ? fmt(v) : NONE;
}

let panelCount = 0;

// buildPanels clones one port panel per port from the template, replacing `{i}` in data-src by the
// index. It runs only when the number of ports changes.
function buildPanels(n) {
  const host = document.getElementById("ports");
  const tpl = document.getElementById("port-panel");
  host.replaceChildren();
  for (let i = 0; i < n; i++) {
    const frag = tpl.content.cloneNode(true);
    for (const el of frag.querySelectorAll("[data-src]")) {
      el.dataset.src = el.dataset.src.replace("{i}", String(i));
    }
    host.appendChild(frag);
  }
  panelCount = n;
}

function paint(state, linkState) {
  const n = state !== null && Array.isArray(state.ports) ? state.ports.length : 0;
  if (n !== panelCount) {
    buildPanels(n);
  }
  for (const el of document.querySelectorAll("[data-src]")) {
    el.textContent = shown(state, el);
  }
  const instance = state !== null && typeof state.instance === "string" ? state.instance : "";
  document.title = instance === "" ? "sil" : "sil — " + instance;
  const link = document.getElementById("link");
  link.textContent = LINK_TEXT[linkState] || LINK_TEXT.connecting;
  link.dataset.state = linkState;
}

let pending = null;
let scheduled = false;

// render shows state (the store's copy, or null before the first snapshot) and linkState
// (connecting, live or retrying). Calls within one frame are coalesced: only the last is painted.
export function render(state, linkState) {
  pending = { state, linkState };
  if (scheduled) {
    return;
  }
  scheduled = true;
  requestAnimationFrame(() => {
    scheduled = false;
    const p = pending;
    pending = null;
    paint(p.state, p.linkState);
  });
}
