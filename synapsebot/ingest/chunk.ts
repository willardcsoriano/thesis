// Splits source files into section-sized chunks for retrieval.
//
// A chunk is one heading's section. Its text is kept as paragraph-sized
// pieces because Claude's search-result citations point at whole pieces:
// finer pieces give tighter citations. Sections longer than MAX_CHUNK_CHARS
// are split into numbered parts so one huge section can't crowd out the rest
// of the retrieval budget.

import { parse, type HTMLElement } from "node-html-parser";
import type { Chunk } from "../src/corpus.js";

export const MAX_CHUNK_CHARS = 3500;

/** Sections that are navigation, not content. */
const SKIPPED_HEADINGS = new Set(["table of contents", "contents"]);

interface Section {
  heading: string;
  anchor: string;
  paragraphs: string[];
}

/** GitHub's heading-anchor slug: lowercase, drop punctuation, spaces to dashes. */
export function slugify(heading: string): string {
  return heading
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .replace(/\s/g, "-");
}

/** Strips inline Markdown that only adds noise to retrieval and quotes. */
function plainInline(text: string): string {
  return text
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/<!--.*?-->/g, "")
    .trim();
}

export function chunkMarkdown(path: string, source: string): Chunk[] {
  const lines = source.replace(/\r\n/g, "\n").split("\n");
  let docTitle = path;
  const sections: Section[] = [{ heading: "", anchor: "", paragraphs: [] }];
  const slugCounts = new Map<string, number>();
  let paragraph: string[] = [];
  let inFence = false;
  let fence = "";

  const current = () => sections[sections.length - 1]!;
  const flush = () => {
    const text = paragraph.join("\n").trim();
    if (text) current().paragraphs.push(inFence ? text : plainInline(text));
    paragraph = [];
  };

  for (const line of lines) {
    const fenceMatch = /^\s*(```|~~~)/.exec(line);
    if (fenceMatch) {
      if (!inFence) {
        flush();
        inFence = true;
        fence = fenceMatch[1]!;
        paragraph.push(line);
        continue;
      }
      if (fenceMatch[1] === fence) {
        paragraph.push(line);
        flush();
        inFence = false;
        continue;
      }
    }
    if (inFence) {
      paragraph.push(line);
      continue;
    }

    const heading = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line);
    if (heading) {
      flush();
      const level = heading[1]!.length;
      const text = plainInline(heading[2]!);
      if (level === 1 && docTitle === path) {
        docTitle = text;
        continue;
      }
      // Duplicate headings get GitHub's -1, -2 ... suffixes.
      const base = slugify(text);
      const seen = slugCounts.get(base) ?? 0;
      slugCounts.set(base, seen + 1);
      sections.push({ heading: text, anchor: seen ? `${base}-${seen}` : base, paragraphs: [] });
      continue;
    }

    if (line.trim() === "") flush();
    else paragraph.push(line);
  }
  flush();

  return toChunks(path, docTitle, sections);
}

/** Text-bearing elements; nested matches are skipped so text isn't counted twice. */
const HTML_BLOCKS = "h1,h2,h3,h4,p,li,pre,blockquote,figcaption,caption,th,td";
const HTML_CONTAINERS = new Set(["p", "li", "pre", "blockquote", "td", "th", "figcaption", "caption"]);

function hasBlockAncestor(el: HTMLElement): boolean {
  for (let node = el.parentNode; node; node = node.parentNode) {
    if (HTML_CONTAINERS.has(node.rawTagName?.toLowerCase() ?? "")) return true;
  }
  return false;
}

export function chunkHtml(path: string, source: string): Chunk[] {
  const root = parse(source, { blockTextElements: { script: false, style: false, pre: true } });
  const docTitle = root.querySelector("title")?.text.trim() || path;
  const sections: Section[] = [{ heading: "", anchor: "", paragraphs: [] }];

  for (const el of root.querySelectorAll(HTML_BLOCKS)) {
    if (hasBlockAncestor(el)) continue;
    const tag = el.rawTagName.toLowerCase();
    const text = el.text.replace(/\s+/g, " ").trim();
    if (!text) continue;
    // h1 is the title page; h2/h3 carry the ids the paper links to.
    if (tag === "h2" || tag === "h3") {
      sections.push({ heading: text, anchor: el.getAttribute("id") ?? "", paragraphs: [] });
    } else {
      sections[sections.length - 1]!.paragraphs.push(text);
    }
  }

  return toChunks(path, docTitle, sections);
}

function toChunks(path: string, docTitle: string, sections: Section[]): Chunk[] {
  const chunks: Chunk[] = [];
  for (const section of sections) {
    if (SKIPPED_HEADINGS.has(section.heading.toLowerCase())) continue;
    if (section.paragraphs.length === 0) continue;
    const title = section.heading ? `${docTitle} › ${section.heading}` : docTitle;
    const parts = splitParagraphs(section.paragraphs);
    parts.forEach((paragraphs, i) => {
      const suffix = parts.length > 1 ? `~${i + 1}` : "";
      chunks.push({
        id: `${path}#${section.anchor}${suffix}`,
        path,
        title: parts.length > 1 ? `${title} (part ${i + 1} of ${parts.length})` : title,
        anchor: section.anchor,
        paragraphs,
      });
    });
  }
  return chunks;
}

/** Cuts one oversized paragraph (a long table or code block) at line, then sentence, boundaries. */
export function splitLongParagraph(text: string): string[] {
  if (text.length <= MAX_CHUNK_CHARS) return [text];
  const units = text.includes("\n") ? text.split("\n") : text.split(/(?<=[.!?])\s+/);
  const pieces: string[] = [];
  let piece = "";
  for (const unit of units) {
    if (piece && piece.length + unit.length + 1 > MAX_CHUNK_CHARS) {
      pieces.push(piece);
      piece = "";
    }
    piece = piece ? `${piece}${text.includes("\n") ? "\n" : " "}${unit}` : unit;
  }
  if (piece) pieces.push(piece);
  return pieces;
}

/** Groups paragraphs into parts of at most MAX_CHUNK_CHARS. */
function splitParagraphs(paragraphs: string[]): string[][] {
  const parts: string[][] = [];
  let part: string[] = [];
  let size = 0;
  for (const p of paragraphs.flatMap(splitLongParagraph)) {
    if (part.length && size + p.length > MAX_CHUNK_CHARS) {
      parts.push(part);
      part = [];
      size = 0;
    }
    part.push(p);
    size += p.length;
  }
  if (part.length) parts.push(part);
  return parts;
}
