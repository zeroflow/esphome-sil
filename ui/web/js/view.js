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
  hex: (v) => String(v),
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

const EM_DASH = "\u2014";

// ledColor is the rendered colour of a lens for the raw "#rrggbb" the firmware wrote: display
// gamma per channel, round(255 * (c / 255) ** (1 / 2.2)), so a dim value is visibly dim rather than
// black (presentation only; the hex shown under the lens stays the raw string). L is the glow
// strength, max(r, g, b) / 255 of the gamma colour. null for anything that is not "#rrggbb".
export function ledColor(hex) {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(String(hex));
  if (m === null) {
    return null;
  }
  const [r, g, b] = [m[1], m[2], m[3]].map((h) => Math.round(255 * (parseInt(h, 16) / 255) ** (1 / 2.2)));
  return { r, g, b, L: Math.max(r, g, b) / 255 };
}

// ledCaption is the name of every segment whose index list holds i, in label order and joined
// with ", ", or an em dash when no segment does.
export function ledCaption(i, segments) {
  if (segments === null || typeof segments !== "object") {
    return EM_DASH;
  }
  const names = Object.keys(segments)
    .sort()
    .filter((name) => Array.isArray(segments[name]) && segments[name].includes(i));
  return names.length === 0 ? EM_DASH : names.join(", ");
}

// ledCount is how many lenses the state calls for: -1 before the first snapshot (one connecting
// placeholder), else the length of leds.strip.
function ledCount(state) {
  if (state === null) {
    return -1;
  }
  return state.leds && Array.isArray(state.leds.strip) ? state.leds.strip.length : 0;
}

let lensCount = null;

// buildLeds rebuilds the lens columns from the template when their number changes. Before the
// first snapshot the one lens is a placeholder for values, not a value: it has no data-src and no
// data-fmt, so the rule that every [data-src] element reads `--` stays the single rule.
function buildLeds(n) {
  const host = document.getElementById("leds");
  const tpl = document.getElementById("led");
  host.replaceChildren();
  if (n === -1) {
    const frag = tpl.content.cloneNode(true);
    const hex = frag.querySelector(".led-hex");
    hex.removeAttribute("data-src");
    hex.removeAttribute("data-fmt");
    hex.textContent = "#------";
    frag.querySelector(".led-caption").textContent = EM_DASH;
    frag.querySelector(".lens").setAttribute("aria-label", EM_DASH + " #------");
    host.appendChild(frag);
  } else if (n === 0) {
    const none = document.createElement("span");
    none.className = "led-none";
    none.textContent = "no LED strip";
    host.appendChild(none);
  } else {
    for (let i = 0; i < n; i++) {
      const frag = tpl.content.cloneNode(true);
      const hex = frag.querySelector(".led-hex");
      hex.dataset.src = hex.dataset.src.replace("{i}", String(i));
      host.appendChild(frag);
    }
  }
  lensCount = n;
}

// paintLeds sets, for every lens, the caption (and its title), the aria-label and the lit or dark
// look. The hex text is a value element and is set with the others by the data-src sweep.
function paintLeds(state) {
  if (state === null) {
    return;
  }
  const strip = state.leds && Array.isArray(state.leds.strip) ? state.leds.strip : [];
  const segments = state.leds ? state.leds.segments : null;
  const cols = document.querySelectorAll("#leds .led");
  cols.forEach((col, i) => {
    const lens = col.querySelector(".lens");
    const caption = ledCaption(i, segments);
    const hex = String(strip[i]);
    const cap = col.querySelector(".led-caption");
    cap.textContent = caption;
    cap.title = caption;
    lens.setAttribute("aria-label", caption + " " + hex);
    const c = ledColor(hex);
    if (c === null || c.L === 0) {
      lens.style.removeProperty("--lens");
      lens.style.removeProperty("--glow");
      lens.classList.remove("lit");
      return;
    }
    const L = c.L;
    const rgb = c.r + ", " + c.g + ", " + c.b;
    lens.style.setProperty("--lens", "rgb(" + rgb + ")");
    lens.style.setProperty(
      "--glow",
      "0 0 " + (4 + 12 * L).toFixed(2) + "px " + (2 * L).toFixed(2) + "px rgba(" + rgb + ", " + (0.6 * L).toFixed(3) + ")",
    );
    lens.classList.add("lit");
  });
}

function paint(state, linkState) {
  const n = state !== null && Array.isArray(state.ports) ? state.ports.length : 0;
  if (n !== panelCount) {
    buildPanels(n);
  }
  const leds = ledCount(state);
  if (leds !== lensCount) {
    buildLeds(leds);
  }
  for (const el of document.querySelectorAll("[data-src]")) {
    el.textContent = shown(state, el);
  }
  const instance = state !== null && typeof state.instance === "string" ? state.instance : "";
  paintLeds(state);
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
