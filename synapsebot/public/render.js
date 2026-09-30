// Renders a model answer (a small Markdown subset) to HTML.
//
// Everything is escaped first and only a fixed set of constructs is turned
// back into markup, so text from the model can never inject HTML. Footnote
// markers arrive in the text as <n> and become superscript links.

const CITE = /(\d+)/g;

export function citeMarker(ref) {
  return `${ref}`;
}

export function stripCiteMarkers(text) {
  return text.replace(CITE, "");
}

function escapeHtml(text) {
  return text.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function inline(text, entryId) {
  // Split out code spans first so emphasis rules don't touch their contents.
  return text
    .split(/(`[^`\n]+`)/)
    .map((part) => {
      if (/^`[^`\n]+`$/.test(part)) return `<code>${escapeHtml(part.slice(1, -1)).replace(CITE, "")}</code>`;
      return escapeHtml(part)
        .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
        .replace(/(^|[^*\w])\*([^*\n]+)\*(?!\w)/g, "$1<em>$2</em>")
        .replace(/(^|[^_\w])_([^_\n]+)_(?!\w)/g, "$1<em>$2</em>")
        .replace(CITE, (_, n) => `<sup><a href="#${entryId}-fn-${n}" aria-label="Source ${n}">${n}</a></sup>`);
    })
    .join("");
}

export function renderAnswer(markdown, entryId) {
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  const out = [];
  let paragraph = [];
  let list = null; // { ordered, items }
  let fence = null; // lines inside ``` ... ```

  const flushParagraph = () => {
    if (paragraph.length) out.push(`<p>${inline(paragraph.join(" "), entryId)}</p>`);
    paragraph = [];
  };
  const flushList = () => {
    if (!list) return;
    const tag = list.ordered ? "ol" : "ul";
    out.push(`<${tag}>${list.items.map((item) => `<li>${inline(item, entryId)}</li>`).join("")}</${tag}>`);
    list = null;
  };

  for (const line of lines) {
    if (fence) {
      if (/^\s*```/.test(line)) {
        out.push(`<pre><code>${escapeHtml(fence.join("\n")).replace(CITE, "")}</code></pre>`);
        fence = null;
      } else {
        fence.push(line);
      }
      continue;
    }
    if (/^\s*```/.test(line)) {
      flushParagraph();
      flushList();
      fence = [];
      continue;
    }
    const bullet = /^\s*[-*]\s+(.*)$/.exec(line);
    const numbered = /^\s*\d+[.)]\s+(.*)$/.exec(line);
    if (bullet || numbered) {
      flushParagraph();
      const ordered = Boolean(numbered);
      if (list && list.ordered !== ordered) flushList();
      list ??= { ordered, items: [] };
      list.items.push((bullet ?? numbered)[1]);
      continue;
    }
    const heading = /^\s*#{1,6}\s+(.*)$/.exec(line);
    if (heading) {
      flushParagraph();
      flushList();
      out.push(`<p><strong>${inline(heading[1], entryId)}</strong></p>`);
      continue;
    }
    if (line.trim() === "") {
      flushParagraph();
      flushList();
      continue;
    }
    if (list) {
      // A continuation line belongs to the last list item.
      list.items[list.items.length - 1] += ` ${line.trim()}`;
      continue;
    }
    paragraph.push(line.trim());
  }
  // An unclosed fence mid-stream still shows its contents.
  if (fence) out.push(`<pre><code>${escapeHtml(fence.join("\n")).replace(CITE, "")}</code></pre>`);
  flushParagraph();
  flushList();
  return out.join("");
}
