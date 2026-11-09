// Every request the page makes to the instance goes through this table and call(). Each entry is
// [method, path] of a row of the Go operation table (internal/control/operations.go) that the LAN
// serves; ui/ui_test.go checks that, so the page cannot use an operation the CLI does not have.
export const OPS = {
  fans: ["GET", "/api/v1/fans"],
  set: ["POST", "/api/v1/set"],
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

// call performs the operation `name` of OPS, with an optional JSON body, and returns the parsed JSON
// answer. A response that is not ok throws ApiError; a request that got no response at all rejects
// with the browser's own error, which callers do not show (LINK reports the connection).
export async function call(name, { body } = {}) {
  const op = OPS[name];
  if (!op) {
    throw new TypeError("not an operation of the page: " + name);
  }
  const [method, path] = op;
  const init = { method, headers: {} };
  if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
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
