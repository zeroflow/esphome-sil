// Wiring of the page: the WebSocket link to the instance and its reconnect loop. Frames are
// `sil watch` lines; store.js folds them into the status mirror.
import { ApiError, call } from "./api.js";
import { onFans } from "./inputs.js";
import { showError } from "./message.js";
import { apply, getState } from "./store.js";
import { render } from "./view.js";

const RETRY_MIN_MS = 500;
const RETRY_MAX_MS = 5000;

let socket = null;
let delay = RETRY_MIN_MS;
let linkState = "connecting";

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
    onFans(res.fans);
  } catch (err) {
    onFans(null);
    if (err instanceof ApiError) {
      showError(err.message);
    }
  }
  refresh();
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
  if (frame && frame.error) {
    showError(String(frame.error.message));
    return;
  }
  const res = apply(frame);
  if (frame && frame.k === "snapshot") {
    delay = RETRY_MIN_MS;
    linkState = "live";
  }
  refresh();
  if (frame && frame.k === "snapshot") {
    loadFans();
  }
  if (res.resync) {
    lost(ws);
  }
}

function connect() {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(scheme + "//" + location.host + "/ws");
  socket = ws;
  ws.onmessage = (ev) => onFrame(ws, ev.data);
  ws.onclose = () => lost(ws);
  ws.onerror = () => lost(ws);
}

refresh();
connect();
