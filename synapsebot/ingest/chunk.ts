// Splits source files into section-sized chunks for retrieval.
//
// A chunk is one heading's section. Its text is kept as paragraph-sized
// pieces because Claude's search-result citations point at whole pieces:
// finer pieces give tighter citations. Sections longer than MAX_CHUNK_CHARS
// are split into numbered parts so one huge section can't crowd out the rest
// of the retrieval budget.

import { strFromU8, unzipSync } from "fflate";
import { parse, type HTMLElement } from "node-html-parser";
import type { Chunk } from "../src/corpus.js";

export const MAX_CHUNK_CHARS = 3500;

/** Sections that are navigation, not content. */
const SKIPPED_HEADINGS = new Set(["table of contents", "contents"]);

interface Section {
  heading: string;
  /** Deep-link anchor in the rendered file; empty when there is none. */
  anchor: string;
  /** Unique id key when several sections share one anchor (FAQ questions, slides). */
  key?: string;
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

/** "docs/notes/groupmate-faq.md" -> "Groupmate faq": a readable fallback title. */
export function titleFromPath(path: string): string {
  const name = path.split("/").pop()!.replace(/\.[^.]+$/, "").replace(/[-_]+/g, " ").trim();
  return name.charAt(0).toUpperCase() + name.slice(1);
}

export function chunkMarkdown(path: string, source: string): Chunk[] {
  const lines = source.replace(/\r\n/g, "\n").split("\n");
  const fallbackTitle = titleFromPath(path);
  let docTitle = fallbackTitle;
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
      if (level === 1 && docTitle === fallbackTitle) {
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

    // A line that is only a bold question (FAQ style) gets its own section, so
    // retrieval can match one question and its answer. It links to the
    // enclosing heading, since bold text has no anchor of its own.
    const question = /^\*\*(.+\?)\*\*\s*$/.exec(line.trim());
    if (question) {
      flush();
      const parent = sections.findLast((sec) => !sec.key)!;
      const text = plainInline(question[1]!);
      sections.push({
        heading: parent.heading ? `${parent.heading} › ${text}` : text,
        anchor: parent.anchor,
        key: `${parent.anchor}/${slugify(text)}`,
        paragraphs: [],
      });
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

const XML_ENTITIES: Record<string, string> = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };

function decodeXml(text: string): string {
  return text.replace(/&(#x[0-9a-f]+|#\d+|\w+);/gi, (whole, ent: string) => {
    if (ent[0] === "#") {
      const code = ent[1]?.toLowerCase() === "x" ? parseInt(ent.slice(2), 16) : parseInt(ent.slice(1), 10);
      return Number.isFinite(code) ? String.fromCodePoint(code) : whole;
    }
    return XML_ENTITIES[ent] ?? whole;
  });
}

/** The text of each <a:p> paragraph in a DrawingML part, in order. */
function drawingParagraphs(xml: string): string[] {
  const out: string[] = [];
  for (const [, body] of xml.matchAll(/<a:p>([\s\S]*?)<\/a:p>/g)) {
    const text = [...body!.matchAll(/<a:t>([^<]*)<\/a:t>/g)].map((m) => decodeXml(m[1]!)).join("").trim();
    if (text) out.push(text);
  }
  return out;
}

/** All text runs inside an XML fragment, joined with spaces. */
function runs(xml: string): string {
  return [...xml.matchAll(/<a:t>([^<]*)<\/a:t>/g)].map((m) => decodeXml(m[1]!)).join(" ").replace(/\s+/g, " ").trim();
}

/** The cached values of a chart reference (<c:cat> or <c:val>), in point order. */
function chartPoints(xml: string | undefined): string[] {
  if (!xml) return [];
  return [...xml.matchAll(/<c:pt idx="\d+">\s*<c:v>([^<]*)<\/c:v>/g)].map((m) => decodeXml(m[1]!));
}

/**
 * A chart's numbers as one sentence, e.g. "Chart: Lower is better… Silent
 * data loss (%): Pattern list 41.2, Fail-closed list 0, Algorithm 0." Charts
 * keep their data in their own part, so slide text alone never shows it.
 */
export function describeChart(xml: string): string {
  const [head = "", plot = ""] = xml.split("<c:plotArea");
  const parts: string[] = [];
  const title = runs(head);
  for (const [, ser] of plot.matchAll(/<c:ser>([\s\S]*?)<\/c:ser>/g)) {
    const name = decodeXml(/<c:tx>[\s\S]*?<c:v>([^<]*)<\/c:v>/.exec(ser!)?.[1] ?? "").trim();
    const cats = chartPoints(/<c:cat>([\s\S]*?)<\/c:cat>/.exec(ser!)?.[1]);
    const vals = chartPoints(/<c:val>([\s\S]*?)<\/c:val>/.exec(ser!)?.[1]);
    const points = vals.map((v, i) => (cats[i] ? `${cats[i]} ${v}` : v)).join(", ");
    if (points) parts.push(name ? `${name}: ${points}` : points);
  }
  const axes = [...plot.matchAll(/<c:(?:valAx|catAx)>[\s\S]*?<c:title>([\s\S]*?)<\/c:title>/g)].map((m) => runs(m[1]!)).filter(Boolean);
  if (!parts.length) return "";
  return `Chart${title ? `: ${title}` : ""}. ${parts.join("; ")}.${axes.length ? ` Axes: ${axes.join(", ")}.` : ""}`;
}

/**
 * One chunk per slide: the slide's text, then any chart data, then its speaker notes. Notes are
 * found through the slide's relationships; bare numbers (the slide-number
 * placeholder in notes) are dropped.
 */
export function chunkPptx(path: string, data: Uint8Array): Chunk[] {
  const files = unzipSync(data);
  const read = (name: string) => (files[name] ? strFromU8(files[name]) : "");
  // Generated decks carry the tool's default title, so the file name names the deck.
  const docTitle = titleFromPath(path);

  const slides = Object.keys(files)
    .map((name) => /^ppt\/slides\/slide(\d+)\.xml$/.exec(name))
    .filter((m): m is RegExpExecArray => m !== null)
    .map((m) => Number(m[1]))
    .sort((a, b) => a - b);

  const sections: Section[] = [];
  for (const n of slides) {
    // Bare numbers are slide-number placeholders, on slides and in notes alike.
    const lines = drawingParagraphs(read(`ppt/slides/slide${n}.xml`)).filter((p) => !/^\d+$/.test(p));
    const rels = read(`ppt/slides/_rels/slide${n}.xml.rels`);
    // Relationship targets may be relative ("../charts/x") or package-absolute ("/ppt/charts/x").
    const notesTarget = /Target="(?:\.\.|\/ppt)\/notesSlides\/([^"]+)"/.exec(rels)?.[1];
    const notes = notesTarget
      ? drawingParagraphs(read(`ppt/notesSlides/${notesTarget}`)).filter((p) => !/^\d+$/.test(p))
      : [];
    const charts = [...rels.matchAll(/Target="(?:\.\.|\/ppt)\/charts\/([^"]+)"/g)]
      .map((m) => describeChart(read(`ppt/charts/${m[1]}`)))
      .filter(Boolean);
    const title = lines[0] ?? `Slide ${n}`;
    const paragraphs = [...lines.slice(1), ...charts];
    if (notes.length) paragraphs.push(`Speaker notes: ${notes.join(" ")}`);
    sections.push({ heading: `Slide ${n}: ${title.trim()}`, anchor: "", key: `slide-${n}`, paragraphs });
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
        id: `${path}#${section.key ?? section.anchor}${suffix}`,
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
