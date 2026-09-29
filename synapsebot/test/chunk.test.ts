import { describe, expect, it } from "vitest";
import { chunkHtml, chunkMarkdown, MAX_CHUNK_CHARS, slugify, splitLongParagraph } from "../ingest/chunk.js";

describe("chunkMarkdown", () => {
  it("makes one chunk per section, titled by document and heading", () => {
    const chunks = chunkMarkdown(
      "docs/x.md",
      "# The Doc\n\n## Overview\n\nFirst para.\n\nSecond para.\n\n## Safety Model\n\nBody.\n",
    );
    expect(chunks.map((c) => [c.id, c.title, c.paragraphs])).toEqual([
      ["docs/x.md#overview", "The Doc › Overview", ["First para.", "Second para."]],
      ["docs/x.md#safety-model", "The Doc › Safety Model", ["Body."]],
    ]);
  });

  it("ignores headings inside code fences and keeps the fence as one paragraph", () => {
    const md = "# T\n\n## Usage\n\n```sh\n# not a heading\n\nmake ci\n```\n\nAfter.\n";
    const [chunk] = chunkMarkdown("a.md", md);
    expect(chunk!.paragraphs).toEqual(["```sh\n# not a heading\n\nmake ci\n```", "After."]);
  });

  it("skips the table of contents and empty sections", () => {
    const md = "# T\n\n## Table of Contents\n\n- [A](#a)\n\n## Empty\n\n## A\n\nText.\n";
    expect(chunkMarkdown("a.md", md).map((c) => c.anchor)).toEqual(["a"]);
  });

  it("numbers duplicate anchors the way GitHub does", () => {
    const md = "## Notes\n\nOne.\n\n## Notes\n\nTwo.\n";
    expect(chunkMarkdown("a.md", md).map((c) => c.anchor)).toEqual(["notes", "notes-1"]);
  });

  it("strips link targets but keeps link text", () => {
    const [chunk] = chunkMarkdown("a.md", "See [the model](safety-model.md#x) now.\n");
    expect(chunk!.paragraphs).toEqual(["See the model now."]);
  });

  it("splits long sections into numbered parts under the size limit", () => {
    const para = "word ".repeat(300).trim();
    const md = `# T\n\n## Big\n\n${Array(8).fill(para).join("\n\n")}\n`;
    const chunks = chunkMarkdown("a.md", md);
    expect(chunks.length).toBeGreaterThan(1);
    expect(chunks[0]!.id).toBe("a.md#big~1");
    expect(chunks[0]!.title).toBe(`T › Big (part 1 of ${chunks.length})`);
    for (const c of chunks) expect(c.paragraphs.join("").length).toBeLessThanOrEqual(MAX_CHUNK_CHARS);
  });
});

describe("splitLongParagraph", () => {
  it("cuts oversized text at line boundaries", () => {
    const table = Array.from({ length: 400 }, (_, i) => `| row ${i} | value |`).join("\n");
    const pieces = splitLongParagraph(table);
    expect(pieces.length).toBeGreaterThan(1);
    expect(pieces.join("\n")).toBe(table);
    for (const p of pieces) expect(p.length).toBeLessThanOrEqual(MAX_CHUNK_CHARS);
  });
});

describe("slugify", () => {
  it("matches GitHub anchors", () => {
    expect(slugify("So — is everything technically undoable?")).toBe("so--is-everything-technically-undoable");
    expect(slugify("1.1 Scope and Limitations")).toBe("11-scope-and-limitations");
  });
});

describe("chunkHtml", () => {
  it("sections on h2/h3 ids, keeps h1 text, and does not double-count nested text", () => {
    const html = `<html><head><title>Paper</title><style>p{}</style></head><body>
      <h1>Title page</h1><p>Front matter.</p>
      <h2 id="sec1">1 Introduction</h2><p>Intro <em>text</em>.</p>
      <ul><li><p>Nested item.</p></li></ul>
      <h3 id="sec1-1">1.1 Scope</h3><table><tr><td>Cell</td></tr></table>
    </body></html>`;
    expect(chunkHtml("paper.html", html).map((c) => [c.id, c.title, c.paragraphs])).toEqual([
      ["paper.html#", "Paper", ["Title page", "Front matter."]],
      ["paper.html#sec1", "Paper › 1 Introduction", ["Intro text.", "Nested item."]],
      ["paper.html#sec1-1", "Paper › 1.1 Scope", ["Cell"]],
    ]);
  });
});
