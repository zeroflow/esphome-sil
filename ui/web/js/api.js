// Every request the page makes to the instance goes through this table and call(). Each entry is
// [method, path] of a row of the Go operation table (internal/control/operations.go) that the LAN
// serves; ui/ui_test.go checks that, so the page cannot use an operation the CLI does not have.
export const OPS = {
  fans: ["GET", "/api/v1/fans"],
  set: ["POST", "/api/v1/set"],
  history: ["GET", "/api/v1/history"],
  "button.down": ["POST", "/api/v1/buttons/{id}/down"],
  "button.up": ["POST", "/api/v1/buttons/{id}/up"],
};

// ApiError is a response that was not a success. Its message is only what the server said: the
// message of a JSON error, or the response body text (possibly empty). The page adds no words.
export class ApiError extends Error {
  constructor(message, code, detail, status) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.detail = detail;
    this.status = status;
  }
}

// call performs the operation `name` of OPS, with an optional JSON body, an optional query object
// ({window_s: 62} becomes ?window_s=62) and the values of the path's {name} parameters ({id: "usr1"}
// fills {id}, percent-encoded), and returns the parsed JSON answer. `keepalive: true` lets the
// request outlive the page (the browser does not cancel it when the tab closes or navigates away; only
// for small bodies). `timeoutMs` bounds the whole call, answer body included: when it elapses the
// request is aborted and the call rejects with the browser's AbortError (no ApiError, so nothing is
// shown; LINK reports the connection). Without it the call waits as long as the browser does. A
// response that is not ok throws ApiError; a request that got no response at all rejects with the
// browser's own error, which callers do not show.
export async function call(name, { body, query, params, keepalive, timeoutMs } = {}) {
  const op = OPS[name];
  if (!op) {
    throw new TypeError("not an operation of the page: " + name);
  }
  const [method, template] = op;
  const base = template.replace(/\{(\w+)\}/g, (_, key) => {
    if (params === undefined || params[key] === undefined) {
      throw new TypeError("missing path parameter " + key + " for " + name);
    }
    return encodeURIComponent(params[key]);
  });
  const path = query === undefined ? base : base + "?" + new URLSearchParams(query).toString();
  const init = { method, headers: {}, keepalive: keepalive === true };
  if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  if (timeoutMs === undefined) {
    return perform(path, init);
  }
  const ctl = new AbortController();
  init.signal = ctl.signal;
  const timer = setTimeout(() => ctl.abort(), timeoutMs);
  try {
    return await perform(path, init);
  } finally {
    clearTimeout(timer);
  }
}

// perform sends the request and turns the response into the parsed answer or an ApiError.
async function perform(path, init) {
  const res = await fetch(path, init);
  if (res.ok) {
    return res.json();
  }
  const text = await res.text();
  let parsed = null;
  try {
    parsed = JSON.parse(text);
  } catch (e) {
    parsed = null;
  }
  const err = parsed && typeof parsed === "object" ? parsed.error : null;
  if (err && typeof err === "object" && typeof err.message === "string") {
    throw new ApiError(err.message, typeof err.code === "string" ? err.code : "", err.detail || null, res.status);
  }
  throw new ApiError(text.trim(), "", null, res.status);
}
