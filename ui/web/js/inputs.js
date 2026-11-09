// The page's controls that change the bench through `set`: the fan select of every port and the
// environment fields. The page validates nothing: a choice or a typed text goes to the server as it
// is, the server answers, and the control then shows what the server reports (the status mirror in
// store.js), never what was asked for.
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

// The environment fields. A field is dirty from its first edit until it is committed or reverted;
// a dirty or focused field is never overwritten by a server value. A field whose commit just
// succeeded is `following`: it takes the server value again even while it still has focus, until
// the user edits it again or leaves it.
const following = new WeakSet();

// lastShown is the shown() callback of the latest render: the formatted server value of an element.
let lastShown = null;

function envFields() {
  return [document.getElementById("temperature"), document.getElementById("humidity")].filter((el) => el !== null);
}

// serverValue is the text of the server's value for a field, or null before the first render.
function serverValue(el) {
  return lastShown === null ? null : lastShown(el);
}

// revert puts the server value back in a field and clears its dirty marker and outline.
function revert(el) {
  el.classList.remove("dirty", "error");
  following.delete(el);
  const v = serverValue(el);
  if (v !== null) {
    el.value = v;
  }
}

// commit sends the given fields in one set, each as the text the user typed. On success the fields
// take the value the server reports it applied and lose the dirty marker; on a refusal the
// server's message goes to the message line, the field the error names (all of them when it names
// none) keeps the typed text and gets the red outline until it is edited again.
async function commit(els) {
  if (els.length === 0) {
    return;
  }
  const values = {};
  for (const el of els) {
    values[el.id] = el.value;
  }
  let res;
  try {
    res = await call("set", { body: { values } });
  } catch (err) {
    if (err instanceof ApiError) {
      showError(err.message);
      const key = err.detail ? err.detail.key : undefined;
      const named = els.filter((el) => el.id === key);
      for (const el of named.length > 0 ? named : els) {
        el.classList.add("error");
      }
    }
    return;
  }
  const applied = res && res.applied ? res.applied : {};
  for (const el of els) {
    el.classList.remove("dirty", "error");
    following.add(el);
    if (applied[el.id] !== undefined && applied[el.id] !== null) {
      el.value = String(applied[el.id]);
    }
  }
}

// initEnv wires the environment fields and APPLY VALUES: Enter commits its own field, APPLY VALUES
// all dirty fields, Escape reverts a field. Typing sends nothing.
export function initEnv() {
  for (const el of envFields()) {
    el.addEventListener("input", () => {
      el.classList.add("dirty");
      el.classList.remove("error");
      following.delete(el);
    });
    el.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        if (el.classList.contains("dirty")) {
          commit([el]);
        }
      } else if (ev.key === "Escape") {
        revert(el);
      }
    });
    el.addEventListener("blur", () => {
      following.delete(el);
      if (!el.classList.contains("dirty")) {
        revert(el);
      }
    });
  }
  const apply = document.getElementById("apply");
  if (apply !== null) {
    apply.addEventListener("click", () => commit(envFields().filter((el) => el.classList.contains("dirty"))));
  }
}

// renderInputs paints the controls from state. shown(el) is the formatted text of an element's
// data-src (`--` while the state does not hold it).
export function renderInputs(state, linkState, shown) {
  const live = linkState === "live";
  lastShown = shown;
  for (const el of envFields()) {
    const protectedField = el.classList.contains("dirty") || (document.activeElement === el && !following.has(el));
    if (!protectedField) {
      el.value = shown(el);
    }
    el.disabled = !live;
  }
  const apply = document.getElementById("apply");
  if (apply !== null) {
    apply.disabled = !live;
  }
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
