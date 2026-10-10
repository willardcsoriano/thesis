// Turns one question and its answer into text worth pasting into a chat or a
// document. Two forms go on the clipboard together: plain text for chat apps
// (footnotes as [1], links listed at the end) and HTML for editors like Google
// Docs (bold question, real links). The receiving app picks the one it uses.

import { renderAnswer } from "./render.js";

const CITE = /(\d+)/g;

/** Footnote markers become plain "[n]" so they survive any paste target. */
function bracketCites(text) {
  return text.replace(CITE, "[$1]");
}

function escapeHtml(text) {
  return text.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

/**
 * @param {{ question: string, mode: string, principle: string, practice: string,
 *           sources: Array<{ ref: number, title: string, path: string, url: string | null }>,
 *           commit?: string }} qa
 */
function parts(qa) {
  const principle = bracketCites(qa.principle).trim();
  const practice = bracketCites(qa.practice).trim();
  const sources = [...qa.sources].sort((a, b) => a.ref - b.ref);
  const footer = `SynapseBot${qa.mode === "study" ? " (study)" : ""}${qa.commit ? ` · thesis at ${qa.commit.slice(0, 7)}` : ""}`;
  return { principle, practice, sources, footer };
}

export function shareText(qa) {
  const { principle, practice, sources, footer } = parts(qa);
  const lines = [`Q: ${qa.question.trim()}`, ""];
  if (practice) lines.push(`In principle: ${principle}`, "", `In practice: ${practice}`);
  else lines.push(principle);
  if (sources.length) {
    lines.push("", "Sources:");
    for (const s of sources) lines.push(`[${s.ref}] ${s.title}${s.url ? ` — ${s.url}` : ` (${s.path})`}`);
  }
  lines.push("", `— ${footer}`);
  return lines.join("\n");
}

export function shareHtml(qa) {
  const { principle, practice, sources, footer } = parts(qa);
  // Cites are already "[n]", so renderAnswer produces no in-page footnote links.
  const answer = practice
    ? `<p><em>In principle</em></p>${renderAnswer(principle, "share")}<p><em>In practice</em></p>${renderAnswer(practice, "share")}`
    : renderAnswer(principle, "share");
  const list = sources.length
    ? `<p><em>Sources</em></p><ol>${sources
        .map((s) => `<li>${s.url ? `<a href="${escapeHtml(s.url)}">${escapeHtml(s.title)}</a>` : escapeHtml(s.title)} <code>${escapeHtml(s.path)}</code></li>`)
        .join("")}</ol>`
    : "";
  return `<p><strong>Q: ${escapeHtml(qa.question.trim())}</strong></p>${answer}${list}<p><em>— ${escapeHtml(footer)}</em></p>`;
}

/** Copies both forms; falls back to plain text where rich clipboard writes aren't supported. */
export async function copyQa(qa) {
  const text = shareText(qa);
  if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
    try {
      await navigator.clipboard.write([
        new ClipboardItem({
          "text/plain": new Blob([text], { type: "text/plain" }),
          "text/html": new Blob([shareHtml(qa)], { type: "text/html" }),
        }),
      ]);
      return;
    } catch {
      // Some browsers refuse rich writes; plain text below still works.
    }
  }
  await navigator.clipboard.writeText(text);
}
