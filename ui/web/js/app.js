// Wiring of the page: the WebSocket link to the instance and its reconnect loop. Frames are
// `sil watch` lines; store.js folds them into the status mirror.
import { apply } from "./store.js";

const RETRY_MIN_MS = 500;
const RETRY_MAX_MS = 5000;

const link = document.getElementById("link");
const message = document.getElementById("message");

let socket = null;
let delay = RETRY_MIN_MS;

function setLink(text) {
  link.textContent = text;
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
  setLink("DISCONNECTED, RETRYING");
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
    message.textContent = String(frame.error.message);
    return;
  }
  const res = apply(frame);
  if (frame && frame.k === "snapshot") {
    delay = RETRY_MIN_MS;
    setLink("LIVE");
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

connect();
