// The page's controls that change the bench through `set`: the fan select of every port. The page
// validates nothing: a choice goes to the server as it is, the server answers, and the control then
// shows what the server reports (the status mirror in store.js), never what was asked for.
import { ApiError, call } from "./api.js";
import { showError } from "./message.js";
import { getState } from "./store.js";

const ERROR_MS = 5000;

// fans is the last GET /api/v1/fans list ([{id, name, ...}]), or null before it was fetched and
// after a failed fetch.
let fans = null;

// pending maps a select to the fan id the user asked for while that change is in flight: the
// select keeps showing it until the server state reports it, or the call fails.
const pending = new WeakMap();

const errorTimers = new WeakMap();

// onFans takes the fan list of a (re)connect; null when the fetch failed.
export function onFans(list) {
  fans = Array.isArray(list) ? list : null;
}

// flagError puts the red outline on el for five seconds.
function flagError(el) {
  clearTimeout(errorTimers.get(el));
  el.classList.add("error");
  errorTimers.set(
    el,
    setTimeout(() => el.classList.remove("error"), ERROR_MS),
  );
}

// portIndex is the index into state.ports of the panel a select belongs to ("ports.<i>.fan").
function portIndex(select) {
  return Number(select.dataset.src.split(".")[1]);
}

// initFanSelects wires the change handler of every fan select under root (a panel or a fragment of
// panels about to be inserted).
export function initFanSelects(root) {
  for (const select of root.querySelectorAll("select.fan")) {
    select.addEventListener("change", () => changeFan(select));
  }
}

// changeFan sends one set for the fan the user chose, at once and without confirmation.
async function changeFan(select) {
  const state = getState();
  const index = portIndex(select);
  const port = state && state.ports[index] ? state.ports[index].port : index + 1;
  const id = select.value;
  pending.set(select, id);
  try {
    await call("set", { body: { values: { ["port" + port + ".fan"]: id } } });
  } catch (err) {
    pending.delete(select);
    const now = getState();
    if (now && now.ports[index]) {
      select.value = String(now.ports[index].fan);
    }
    if (err instanceof ApiError) {
      showError(err.message);
      flagError(select);
    }
  }
}

// fillOptions makes the options of a select: one per library fan (text the fan's name, value and
// title its id) and, when the server's current fan is not in the list, one for it by its id. It
// touches the DOM only when the list changed, so an open drop-down is not closed by a repaint.
function fillOptions(select, current) {
  const list = fans === null ? [] : fans.map((f) => ({ id: String(f.id), name: String(f.name) }));
  if (!list.some((o) => o.id === current)) {
    list.push({ id: current, name: current });
  }
  const sig = JSON.stringify(list);
  if (select.dataset.options === sig) {
    return;
  }
  const options = list.map((o) => {
    const option = document.createElement("option");
    option.value = o.id;
    option.title = o.id;
    option.textContent = o.name;
    return option;
  });
  select.replaceChildren(...options);
  select.dataset.options = sig;
}

// renderInputs paints the selects from state. shown(el) is the formatted text of an element's
// data-src (`--` while the state does not hold it).
export function renderInputs(state, linkState, shown) {
  const live = linkState === "live";
  document.querySelectorAll("#ports select.fan").forEach((select) => {
    const current = shown(select);
    const index = portIndex(select);
    const port = state !== null && state.ports && state.ports[index] ? state.ports[index].port : index + 1;
    select.setAttribute("aria-label", "Fan on port " + port);
    fillOptions(select, current);
    if (pending.get(select) === current) {
      pending.delete(select);
    }
    const want = pending.has(select) ? pending.get(select) : current;
    if (select.value !== want) {
      select.value = want;
    }
    select.disabled = !(live && fans !== null);
  });
}
