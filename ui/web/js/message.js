// The header message line. It shows only text the server supplied, verbatim, for five seconds; the
// page composes no words of its own for it. A newer error replaces the text and restarts the timer.
const SHOW_MS = 5000;

const line = document.getElementById("message");
let timer = null;

// showError shows text (an API error message) in the line. An empty text shows nothing.
export function showError(text) {
  if (typeof text !== "string" || text === "") {
    return;
  }
  clearTimeout(timer);
  line.textContent = text;
  line.title = text;
  line.classList.add("error");
  line.setAttribute("role", "alert");
  timer = setTimeout(clearMessage, SHOW_MS);
}

// clearMessage empties the line and restores the quiet role.
export function clearMessage() {
  clearTimeout(timer);
  timer = null;
  line.textContent = "";
  line.removeAttribute("title");
  line.classList.remove("error");
  line.setAttribute("role", "status");
}
