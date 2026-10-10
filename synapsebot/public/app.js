// SynapseBot page: sends questions to /api/ask and renders the streamed,
// footnoted answers. No framework and no build step.

import { citeMarker, renderAnswer, stripCiteMarkers } from "./render.js";

const CODE_KEY = "synapsebot.access-code";
const MODE_KEY = "synapsebot.mode";
const MAX_HISTORY_PAIRS = 3;

const transcript = document.getElementById("transcript");
const composer = document.getElementById("composer");
const questionBox = document.getElementById("question");
const askButton = document.getElementById("ask");
const accessForm = document.getElementById("access");
const accessInput = document.getElementById("access-code");
const accessError = document.getElementById("access-error");

/** Completed exchanges sent back for follow-up questions: [{ role, text }]. */
const history = [];
let entryCount = 0;
let inFlight = null; // AbortController of the answer being streamed
let queuedQuestion = null; // asked before an access code was entered

// sessionStorage can throw (private mode, blocked storage); the code then
// lasts until the page is closed, which is fine.
function loadCode() {
  try { return sessionStorage.getItem(CODE_KEY) ?? ""; } catch { return ""; }
}
function saveCode(code) {
  try {
    if (code) sessionStorage.setItem(CODE_KEY, code);
    else sessionStorage.removeItem(CODE_KEY);
  } catch { /* storage unavailable */ }
}
let accessCode = loadCode();

// Answer style: "quick" for live questions, "study" for learning. Remembered
// per browser; storage failures just mean it resets to quick next visit.
const modeButtons = [...document.querySelectorAll(".mode [data-mode]")];
let mode = "quick";
try { if (localStorage.getItem(MODE_KEY) === "study") mode = "study"; } catch { /* storage unavailable */ }

function showMode() {
  for (const button of modeButtons) button.setAttribute("aria-checked", String(button.dataset.mode === mode));
  questionBox.placeholder = mode === "study" ? "What do you want to understand?" : "Type the question…";
}

for (const button of modeButtons) {
  button.addEventListener("click", () => {
    mode = button.dataset.mode;
    try { localStorage.setItem(MODE_KEY, mode); } catch { /* storage unavailable */ }
    showMode();
    questionBox.focus();
  });
}
showMode();

function showAccess(message = "") {
  accessForm.hidden = false;
  composer.hidden = true;
  accessError.textContent = message;
  accessInput.value = "";
  accessInput.focus();
}

function hideAccess() {
  accessForm.hidden = true;
  composer.hidden = false;
}

accessForm.addEventListener("submit", (event) => {
  event.preventDefault();
  accessCode = accessInput.value.trim();
  if (!accessCode) return;
  saveCode(accessCode);
  hideAccess();
  if (queuedQuestion) {
    const q = queuedQuestion;
    queuedQuestion = null;
    ask(q);
  } else {
    questionBox.focus();
  }
});

composer.addEventListener("submit", (event) => {
  event.preventDefault();
  if (inFlight) {
    inFlight.abort();
    return;
  }
  const question = questionBox.value.trim();
  if (question) ask(question);
});

questionBox.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault();
    composer.requestSubmit();
  }
});

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function newEntry(question) {
  const id = `entry-${++entryCount}`;
  const entry = el("article", "entry");
  entry.id = id;
  entry.append(el("p", "question", question));
  // An answer has an "In principle" part, always shown, and an optional
  // "In practice" part, folded until the reader opens it. Each part is
  // re-rendered on its own so opening the fold mid-stream stays open.
  const answer = el("div", "answer pending");
  const principleLabel = el("p", "part-label", "In principle");
  principleLabel.hidden = true;
  const principle = el("div", "part");
  const practice = el("details", "practice");
  practice.hidden = true;
  const practiceBody = el("div", "part");
  practice.append(el("summary", "part-label", "In practice"), practiceBody);
  answer.append(principleLabel, principle, practice);
  const notes = el("ol", "footnotes");
  notes.hidden = true;
  const status = el("p", "notice");
  status.hidden = true;
  entry.append(answer, status, notes);
  transcript.append(entry);
  return { id, entry, answer, principleLabel, principle, practice, practiceBody, notes, status };
}

function addFootnote(view, source) {
  const item = el("li");
  item.id = `${view.id}-fn-${source.ref}`;
  item.append(el("span", "num", `${source.ref}.`));
  const body = el("span");
  const title = source.url ? el("a", "", source.title) : el("span", "", source.title);
  if (source.url) {
    title.href = source.url;
    title.target = "_blank";
    title.rel = "noopener noreferrer";
  }
  body.append(title, el("span", "path", source.path));
  item.append(body);
  view.notes.append(item);
  view.notes.hidden = false;
}

function setBusy(busy) {
  askButton.textContent = busy ? "Stop" : "Ask";
  questionBox.readOnly = busy;
}

async function ask(question) {
  if (!accessCode) {
    queuedQuestion = question;
    showAccess();
    return;
  }
  questionBox.value = "";
  const askedMode = mode;
  const view = newEntry(question);
  view.entry.scrollIntoView({ behavior: "smooth", block: "start" });

  const controller = new AbortController();
  inFlight = controller;
  setBusy(true);

  const parts = { principle: "", practice: "" };
  let part = "principle";
  let failed = false;
  const render = () => {
    const body = part === "principle" ? view.principle : view.practiceBody;
    body.innerHTML = renderAnswer(parts[part], view.id);
  };
  const startPractice = () => {
    part = "practice";
    view.principleLabel.hidden = false;
    view.practice.hidden = false;
    // Study answers are the point of asking, so they arrive unfolded.
    if (askedMode === "study") view.practice.open = true;
  };
  const clearAnswer = () => {
    parts.principle = parts.practice = "";
    part = "principle";
    view.principle.replaceChildren();
    view.practiceBody.replaceChildren();
    view.principleLabel.hidden = true;
    view.practice.hidden = true;
    view.notes.replaceChildren();
    view.notes.hidden = true;
  };
  const say = (message) => {
    view.status.textContent = message;
    view.status.hidden = false;
  };

  try {
    const response = await fetch("/api/ask", {
      method: "POST",
      headers: { "content-type": "application/json", "x-access-code": accessCode },
      body: JSON.stringify({ question, mode: askedMode, history: history.slice(-MAX_HISTORY_PAIRS * 2) }),
      signal: controller.signal,
    });

    if (!response.ok) {
      failed = true;
      const { error } = await response.json().catch(() => ({ error: "" }));
      if (response.status === 401) {
        view.entry.remove();
        accessCode = "";
        saveCode("");
        queuedQuestion = question;
        showAccess(error || "That access code is not right.");
        return;
      }
      say(error || `The request failed (${response.status}). Please try again.`);
      return;
    }

    const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
    let buffer = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += value;
      const frames = buffer.split("\n\n");
      buffer = frames.pop();
      for (const frame of frames) {
        if (!frame.startsWith("data: ")) continue;
        const event = JSON.parse(frame.slice(6));
        switch (event.type) {
          case "text": parts[part] += event.text; render(); break;
          case "cite": parts[part] += citeMarker(event.ref); render(); break;
          case "practice": startPractice(); break;
          case "source": addFootnote(view, event); break;
          case "reset": clearAnswer(); break;
          case "notice": say(event.text); break;
          case "error": failed = true; say(event.text); break;
        }
      }
    }
  } catch (err) {
    failed = true;
    say(err.name === "AbortError" ? "Stopped." : "The connection was lost. Please try again.");
  } finally {
    inFlight = null;
    setBusy(false);
    view.answer.classList.remove("pending");
    if (!parts.principle && !parts.practice) view.answer.remove();
    // Sent back with follow-ups in the same two-part form the model writes.
    const answerText = stripCiteMarkers(
      parts.practice ? `${parts.principle.trim()}\n[[practice]]\n${parts.practice.trim()}` : parts.principle,
    ).trim();
    if (!failed && answerText) history.push({ role: "user", text: question }, { role: "assistant", text: answerText });
    if (!accessForm.hidden) return;
    questionBox.focus();
  }
}

async function loadProvenance() {
  try {
    const response = await fetch("/api/meta");
    if (!response.ok) return;
    const meta = await response.json();
    const line = document.getElementById("provenance");
    const date = new Date(meta.builtAt).toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" });
    line.textContent = "From commit ";
    const commit = meta.repoUrl ? el("a") : el("code");
    commit.textContent = meta.commit.slice(0, 7);
    if (meta.repoUrl) commit.href = `${meta.repoUrl}/tree/${meta.commit}`;
    line.append(commit, `${meta.dirty ? " (with uncommitted edits)" : ""} · ${date}`);
  } catch { /* the colophon keeps its generic line */ }
}

// Light/dark toggle. theme.js has already applied a saved choice; with none,
// the page follows the system setting until the reader picks one.
const THEME_KEY = "synapsebot.theme";
const themeToggle = document.getElementById("theme-toggle");
const systemDark = window.matchMedia("(prefers-color-scheme: dark)");

function currentTheme() {
  return document.documentElement.dataset.theme ?? (systemDark.matches ? "dark" : "light");
}

// Line icons in the current text color: the button shows the mode it switches to.
const SUN_ICON =
  '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true">' +
  '<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2.2M12 19.3v2.2M2.5 12h2.2M19.3 12h2.2M5.3 5.3l1.6 1.6M17.1 17.1l1.6 1.6M5.3 18.7l1.6-1.6M17.1 6.9l1.6-1.6"/></svg>';
const MOON_ICON =
  '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" aria-hidden="true">' +
  '<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"/></svg>';

function showThemeToggle() {
  const dark = currentTheme() === "dark";
  themeToggle.innerHTML = dark ? SUN_ICON : MOON_ICON;
  themeToggle.setAttribute("aria-pressed", String(dark));
  themeToggle.setAttribute("aria-label", dark ? "Switch to light mode" : "Switch to dark mode");
}

themeToggle.addEventListener("click", () => {
  const next = currentTheme() === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = next;
  document.documentElement.style.colorScheme = next;
  try { localStorage.setItem(THEME_KEY, next); } catch { /* the choice lasts for this page only */ }
  showThemeToggle();
});
systemDark.addEventListener("change", showThemeToggle);
showThemeToggle();

loadProvenance();
