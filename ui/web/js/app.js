// Wiring of the page: the WebSocket link to the instance and its reconnect loop. Frames are
// `sil watch` lines; store.js folds them into the status mirror.
import { ApiError, call } from "./api.js";
import { initButtons, onHeld, onReleased } from "./buttons.js";
import { initEnv, onFans } from "./inputs.js";
import { showError } from "./message.js";
import { loadHistory, pushSample, resetScopes, setFanMax } from "./plot.js";
import { apply, getState } from "./store.js";
import { render } from "./view.js";

const RETRY_MIN_MS = 500;
const RETRY_MAX_MS = 5000;

// The kinds of `sil watch`, plus the opt-in plot samples.
const KINDS = "duty,rpm,tach,state,fan,load,button,env,leds,firmware,sim,sample";
// The history to fetch on every (re)connect: the 60 s window plus a 2 s margin.
const HISTORY_WINDOW_S = 62;
// The most sample frames held back while the history request is out (5 minutes at 10 Hz, the
// server's ring); a request that long has failed.
const HELD_MAX = 3000;
// The time the history request gets (real time); when it elapses the join fails and the plot goes on
// with live samples only (failJoin), so a request that hangs while the WebSocket stays up cannot hold
// the plot back for good.
const HISTORY_TIMEOUT_MS = 10000;

let socket = null;
let delay = RETRY_MIN_MS;
let linkState = "connecting";

// fanList is the last GET /api/v1/fans list, or null before it was fetched and after a failed fetch.
let fanList = null;

// join is the history join of the current connection: sample frames are held in `held` until
// GET /api/v1/history has answered, then the history is plotted, then the held samples above its
// seq, then (done) every sample goes straight to the plot.
let join = null;

// refresh paints the current state and link state; every applied frame and every link change ends here.
function refresh() {
  render(getState(), linkState);
}

// loadFans fetches the fan library once per connection; the selects of all ports are filled from it.
// A failed fetch leaves every select with only its current fan, disabled, and the server's words
// in the message line.
async function loadFans() {
  try {
    const res = await call("fans");
    fanList = res.fans;
    onFans(res.fans);
  } catch (err) {
    fanList = null;
    onFans(null);
    if (err instanceof ApiError) {
      showError(err.message);
    }
  }
  syncFanMax();
  refresh();
}

// syncFanMax gives every scope the max_rpm of the fan the status says is plugged on its port; a
// port whose fan is not in the list (or no list) has an unknown scale.
function syncFanMax() {
  const state = getState();
  if (state === null || !Array.isArray(state.ports)) {
    return;
  }
  for (const p of state.ports) {
    const fan = Array.isArray(fanList) ? fanList.find((f) => f.id === p.fan) : undefined;
    setFanMax(p.port, fan === undefined ? null : fan.max_rpm);
  }
}

// startJoin begins the history join of a connection. It runs after the snapshot, so the history's
// seq is at least the snapshot's and no sample falls between the history and the stream.
function startJoin(ws) {
  const j = { ws, held: [], done: false };
  join = j;
  call("history", { query: { window_s: HISTORY_WINDOW_S }, timeoutMs: HISTORY_TIMEOUT_MS }).then(
    (res) => finishJoin(j, res),
    (err) => failJoin(j, err),
  );
}

// finishJoin replaces the plot with the history, then plays the held samples the history does not
// hold (seq above its own).
function finishJoin(j, res) {
  if (join !== j) {
    return;
  }
  loadHistory(res);
  for (const frame of j.held) {
    if (frame.seq > res.seq) {
      pushSample(frame);
    }
  }
  j.held = [];
  j.done = true;
}

// failJoin shows the server's words and carries on with live samples only, on empty screens.
function failJoin(j, err) {
  if (join !== j) {
    return;
  }
  if (err instanceof ApiError) {
    showError(err.message);
  }
  resetScopes();
  for (const frame of j.held) {
    pushSample(frame);
  }
  j.held = [];
  j.done = true;
}

// onSample routes one sample frame of the connection ws: held during the join, plotted after.
function onSample(ws, frame) {
  if (join === null || join.ws !== ws) {
    return;
  }
  if (!join.done) {
    join.held.push(frame);
    if (join.held.length > HELD_MAX) {
      join.held.shift();
    }
    return;
  }
  pushSample(frame);
}

function setLink(next) {
  linkState = next;
  refresh();
}

// lost handles the end of one connection (close, error or a frame asking for a resync): the state
// stays for display, and a new connection is tried after the current delay, which doubles up to
// the cap. Each connection is handled once.
function lost(ws) {
  if (ws !== socket) {
    return;
  }
  socket = null;
  if (join !== null && join.ws === ws) {
    join = null;
  }
  try {
    ws.close();
  } catch (e) {
    // already closed
  }
  setLink("retrying");
  setTimeout(connect, delay);
  delay = Math.min(delay * 2, RETRY_MAX_MS);
}

function onFrame(ws, data) {
  let frame;
  try {
    frame = JSON.parse(data);
  } catch (e) {
    lost(ws);
    return;
  }
  if (frame && frame.k === undefined && frame.error) {
    // The stream failed (for instance too_many_streams): its words, then the normal backoff. Only the
    // server's final error frame has `error` and no `k`: the snapshot of a failed instance carries the
    // status error as a top-level `error` too (the embedded Status is promoted), and it is state to show.
    showError(String(frame.error.message));
    lost(ws);
    return;
  }
  const res = apply(frame);
  if (frame && frame.k === "snapshot") {
    delay = RETRY_MIN_MS;
    linkState = "live";
  }
  if (frame && frame.k === "sample") {
    onSample(ws, frame);
  }
  if (frame && frame.k === "fan") {
    syncFanMax();
  }
  if (frame && frame.k === "button") {
    if (frame.v === true) {
      onHeld(frame.id);
    } else if (frame.v === false) {
      onReleased(frame.id);
    }
  }
  refresh();
  if (frame && frame.k === "snapshot") {
    startJoin(ws);
    syncFanMax();
    loadFans();
  }
  if (res.resync) {
    lost(ws);
  }
}

function connect() {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(scheme + "//" + location.host + "/ws?kind=" + KINDS);
  socket = ws;
  ws.onmessage = (ev) => onFrame(ws, ev.data);
  ws.onclose = () => lost(ws);
  ws.onerror = () => lost(ws);
}

initEnv();
initButtons();
refresh();
connect();
