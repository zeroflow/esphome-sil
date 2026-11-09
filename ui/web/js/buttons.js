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
// old down cannot touch a newer hold), `downDone` (a promise of the last down's success) and
// `ownLoss` (pointer ids whose capture this page let go of itself).
const caps = new Map();

// listening is true once the window and document listeners are installed.
let listening = false;

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
  cap.ownLoss.clear();
  cap.downDone.then((started) => {
    if (started) {
      // keepalive: the up is most often sent while the page is being closed (tab hidden, window blur).
      call("button.up", { params: { id: cap.id }, keepalive: true }).catch(say);
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
    // The lostpointercapture this causes (browsers that fire it for an implicit capture; Chrome does
    // not) is not a release of the hold that starts right here.
    cap.ownLoss.add(ev.pointerId);
    cap.el.releasePointerCapture(ev.pointerId);
  }
  down(cap);
}

// onLostCapture releases the hold when the browser took the pointer away from the cap, except for
// the loss the page caused itself in onPointerDown.
function onLostCapture(cap, ev) {
  if (cap.ownLoss.delete(ev.pointerId)) {
    return;
  }
  release(cap);
}

// isHoldKey is true for the keys that hold a cap: Space and Enter.
function isHoldKey(ev) {
  return ev.key === " " || ev.key === "Enter";
}

// onKeyDown starts a hold on the first keydown of Space or Enter; a repeated keydown does nothing.
function onKeyDown(cap, ev) {
  if (!isHoldKey(ev)) {
    return;
  }
  ev.preventDefault();
  if (!ev.repeat) {
    down(cap);
  }
}

// onKeyUp ends the hold of Space or Enter (and keeps the browser from turning the key into a click).
function onKeyUp(cap, ev) {
  if (!isHoldKey(ev)) {
    return;
  }
  ev.preventDefault();
  release(cap);
}

// releaseAll ends every hold this page started.
function releaseAll() {
  for (const cap of caps.values()) {
    release(cap);
  }
}


// initButtons wires every USR cap under root.
export function initButtons(root = document) {
  for (const el of root.querySelectorAll("button.usr")) {
    const cap = {
      id: el.dataset.id,
      el,
      holding: false,
      seenHeld: false,
      gen: 0,
      downDone: Promise.resolve(false),
      ownLoss: new Set(),
    };
    caps.set(cap.id, cap);
    el.addEventListener("pointerdown", (ev) => onPointerDown(cap, ev));
    // Every way a pointer or the keyboard can end a hold, or stop being able to end it.
    for (const type of ["pointerup", "pointercancel", "pointerleave", "blur"]) {
      el.addEventListener(type, () => release(cap));
    }
    el.addEventListener("lostpointercapture", (ev) => onLostCapture(cap, ev));
    el.addEventListener("keydown", (ev) => onKeyDown(cap, ev));
    el.addEventListener("keyup", (ev) => onKeyUp(cap, ev));
    el.addEventListener("contextmenu", (ev) => ev.preventDefault());
  }
  if (!listening) {
    listening = true;
    // A window that loses focus or a tab that goes to the background cannot see the pointer come up.
    window.addEventListener("blur", releaseAll);
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) {
        releaseAll();
      }
    });
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
// disabled unless the link is live. A disabled cap can no longer see its pointer come up, so a hold
// still open when the link is lost is released here (best effort; the engine's max hold is the
// backstop).
export function renderButtons(state, linkState) {
  for (const cap of caps.values()) {
    const v = state !== null && state.buttons ? state.buttons[cap.id] : undefined;
    if (v === undefined || v === null) {
      cap.el.removeAttribute("aria-pressed");
    } else {
      cap.el.setAttribute("aria-pressed", String(v === true));
    }
    cap.el.disabled = linkState !== "live";
    if (cap.el.disabled) {
      release(cap);
    }
  }
}
