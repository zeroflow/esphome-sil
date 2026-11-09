// The USR press-and-hold caps. The page measures no time: a down goes out when the pointer goes
// down, an up when it is released, and the firmware measures the hold. A cap is lit only by the
// server's report (the status mirror), never by a local flag. Every down carries a max hold, so the
// engine releases the button by itself if the page never gets to send its up (D-15).
import { ApiError, call } from "./api.js";
import { showError } from "./message.js";

// The max hold the page asks for with every down: 10 s of sim time, well above the 1.5 s long press.
export const MAX_HOLD_MS = 10000;

// caps maps a button id to its cap: the element, `holding` (this page started a hold it has not
// released), `seenHeld` (the server has reported that hold), `gen` (counts downs, so an answer to an
// old down cannot touch a newer hold) and `downDone` (a promise of the last down's success).
const caps = new Map();

function say(err) {
  if (err instanceof ApiError) {
    showError(err.message);
  }
}

// down starts a hold: one POST button.down with the max hold. A refusal is shown verbatim and the
// cap is left to the server's report.
function down(cap) {
  if (cap.holding || cap.el.disabled) {
    return;
  }
  cap.holding = true;
  cap.seenHeld = false;
  const gen = ++cap.gen;
  cap.downDone = call("button.down", { params: { id: cap.id }, body: { max_hold_ms: MAX_HOLD_MS } }).then(
    () => true,
    (err) => {
      if (cap.gen === gen) {
        cap.holding = false;
      }
      say(err);
      return false;
    },
  );
}

// release ends a hold this page started and has not seen released: one POST button.up, sent after
// the down it ends has been answered (so the two cannot overtake each other on the wire).
function release(cap) {
  if (!cap.holding) {
    return;
  }
  cap.holding = false;
  cap.downDone.then((started) => {
    if (started) {
      call("button.up", { params: { id: cap.id } }).catch(say);
    }
  });
}

// onPointerDown starts a hold for the primary mouse button, a touch or a pen. The browser captures a
// touch pointer to the element it started on, which would keep the cap held while the finger slides
// off it (Research Pitfall 6): the capture is let go so the pointer leaves the cap like any other.
function onPointerDown(cap, ev) {
  if (ev.pointerType === "mouse" && ev.button !== 0) {
    return;
  }
  ev.preventDefault();
  if (cap.el.hasPointerCapture(ev.pointerId)) {
    cap.el.releasePointerCapture(ev.pointerId);
  }
  down(cap);
}

// initButtons wires every USR cap under root.
export function initButtons(root = document) {
  for (const el of root.querySelectorAll("button.usr")) {
    const cap = { id: el.dataset.id, el, holding: false, seenHeld: false, gen: 0, downDone: Promise.resolve(false) };
    caps.set(cap.id, cap);
    el.addEventListener("pointerdown", (ev) => onPointerDown(cap, ev));
    el.addEventListener("pointerup", () => release(cap));
    el.addEventListener("contextmenu", (ev) => ev.preventDefault());
  }
}

// onHeld tells the cap of button id that the server reported it held (a button frame with v true):
// the hold of this page, if any, has been seen. A false frame from before that is the old state, not
// a release.
export function onHeld(id) {
  const cap = caps.get(id);
  if (cap !== undefined) {
    cap.seenHeld = true;
  }
}

// onReleased tells the cap of button id that the server reported it released (a button frame with
// v false). A hold this page saw held is over, so no up is sent for it (the engine's max-hold
// release, or an up from elsewhere).
export function onReleased(id) {
  const cap = caps.get(id);
  if (cap !== undefined && cap.seenHeld) {
    cap.holding = false;
    cap.seenHeld = false;
  }
}

// renderButtons paints the caps from the status mirror: aria-pressed is the server's held flag
// (absent before the first snapshot and for a button the status does not hold), and every cap is
// disabled unless the link is live.
export function renderButtons(state, linkState) {
  for (const cap of caps.values()) {
    const v = state !== null && state.buttons ? state.buttons[cap.id] : undefined;
    if (v === undefined || v === null) {
      cap.el.removeAttribute("aria-pressed");
    } else {
      cap.el.setAttribute("aria-pressed", String(v === true));
    }
    cap.el.disabled = linkState !== "live";
  }
}
