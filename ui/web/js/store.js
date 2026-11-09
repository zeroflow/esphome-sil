// The page's mirror of GET /api/v1/status. A snapshot frame replaces the whole state; every event
// frame after it changes the one field it names. The store holds what the API reported and
// derives nothing: no connected or behind flags, no timing.

let state = null;

function portAt(port) {
  if (!state || !Array.isArray(state.ports)) {
    return null;
  }
  return state.ports.find((p) => p.port === port) || null;
}

// apply folds one frame (a parsed /ws message) into the state. It returns {resync: true} when the
// stream can no longer be trusted to match the server (a simulator restart, dropped events, the
// end of the stream), so the caller reconnects and gets a new snapshot.
export function apply(frame) {
  const none = { resync: false };
  if (frame === null || typeof frame !== "object") {
    return none;
  }
  if (frame.k === "snapshot") {
    const next = structuredClone(frame);
    delete next.t;
    delete next.k;
    delete next.seq;
    state = next;
  } else if (state === null) {
    return none;
  }
  if (typeof frame.t === "number" && state.sim) {
    state.sim.time_s = frame.t / 1e6;
  }
  switch (frame.k) {
    case "snapshot":
      return none;
    case "duty":
    case "rpm":
    case "state":
    case "fan": {
      const p = portAt(frame.port);
      if (p) {
        p[frame.k] = frame.v;
      }
      return none;
    }
    case "button":
      state.buttons = state.buttons || {};
      state.buttons[frame.id] = frame.v;
      return none;
    case "env":
      state.env = state.env || {};
      state.env[frame.id] = frame.v;
      return none;
    case "leds":
      state.leds = state.leds || {};
      state.leds.strip = frame.strip || [];
      return none;
    case "firmware":
      state.firmware = state.firmware || {};
      state.firmware.bus_connected = frame.v;
      return none;
    case "sim":
      return frame.v === "restarted" ? { resync: true } : none;
    case "lagged":
    case "stopped":
      return { resync: true };
    default:
      return none;
  }
}

// getState returns a copy of the state, or null before the first snapshot.
export function getState() {
  return state === null ? null : structuredClone(state);
}

// reset forgets the state (back to before the first snapshot).
export function reset() {
  state = null;
}
