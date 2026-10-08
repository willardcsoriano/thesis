import { describe, expect, it } from "vitest";
import { strToU8, zipSync } from "fflate";
import { chunkHtml, chunkMarkdown, chunkPptx, describeChart, MAX_CHUNK_CHARS, slugify, splitLongParagraph, titleFromPath } from "../ingest/chunk.js";

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

describe("FAQ-style questions", () => {
  it("gives each bold question its own chunk, linked to the enclosing heading", () => {
    const md = "## 2. Safety\n\nIntro.\n\n**How do you stop blind approval?**\nThe gate asks.\n\n**What can be undone?**\nFile deletions.\n";
    expect(chunkMarkdown("faq.md", md).map((c) => [c.id, c.title, c.anchor, c.paragraphs])).toEqual([
      ["faq.md#2-safety", "Faq › 2. Safety", "2-safety", ["Intro."]],
      ["faq.md#2-safety/how-do-you-stop-blind-approval", "Faq › 2. Safety › How do you stop blind approval?", "2-safety", ["The gate asks."]],
      ["faq.md#2-safety/what-can-be-undone", "Faq › 2. Safety › What can be undone?", "2-safety", ["File deletions."]],
    ]);
  });

  it("leaves bold statements that are not questions alone", () => {
    expect(chunkMarkdown("a.md", "## A\n\n**Important.**\nText.\n").map((c) => c.id)).toEqual(["a.md#a"]);
  });
});

describe("titleFromPath", () => {
  it("turns a file name into a readable title", () => {
    expect(titleFromPath("docs/notes/groupmate-faq.md")).toBe("Groupmate faq");
    expect(titleFromPath("thesis 1/defense/SynapseOS_Proposal_Defense.pptx")).toBe("SynapseOS Proposal Defense");
  });
});

describe("chunkPptx", () => {
  const para = (text: string) => `<a:p><a:r><a:t>${text}</a:t></a:r></a:p>`;
  const deck = zipSync({
    "ppt/slides/slide2.xml": strToU8(`<p:sld>${para("The Gap")}${para("CLI &amp; GUI both exclude")}${para("2")}</p:sld>`),
    "ppt/slides/slide1.xml": strToU8(`<p:sld>${para("SynapseOS")}</p:sld>`),
    "ppt/slides/_rels/slide2.xml.rels": strToU8('<Relationships><Relationship Target="../notesSlides/notesSlide7.xml"/></Relationships>'),
    "ppt/notesSlides/notesSlide7.xml": strToU8(`<p:notes>${para("Nobody built this yet.")}${para("2")}</p:notes>`),
  });

  it("makes one chunk per slide in order, with notes and without slide numbers", () => {
    expect(chunkPptx("d/My_Deck.pptx", deck).map((c) => [c.id, c.title, c.paragraphs])).toEqual([
      ["d/My_Deck.pptx#slide-2", "My Deck › Slide 2: The Gap", ["CLI & GUI both exclude", "Speaker notes: Nobody built this yet."]],
    ]);
  });
});

describe("slide charts", () => {
  const chart = `<c:chartSpace><c:chart><c:title><a:p><a:r><a:t>Lower is better</a:t></a:r></a:p></c:title><c:plotArea>
    <c:barChart><c:ser><c:tx><c:strRef><c:strCache><c:pt idx="0"><c:v>Silent loss (%)</c:v></c:pt></c:strCache></c:strRef></c:tx>
    <c:cat><c:strRef><c:strCache><c:pt idx="0"><c:v>Pattern list</c:v></c:pt><c:pt idx="1"><c:v>Algorithm</c:v></c:pt></c:strCache></c:strRef></c:cat>
    <c:val><c:numRef><c:numCache><c:pt idx="0"><c:v>41.2</c:v></c:pt><c:pt idx="1"><c:v>0</c:v></c:pt></c:numCache></c:numRef></c:val>
    </c:ser></c:barChart></c:plotArea></c:chart></c:chartSpace>`;

  it("describes a chart's title, series, and values", () => {
    expect(describeChart(chart)).toBe("Chart: Lower is better. Silent loss (%): Pattern list 41.2, Algorithm 0.");
  });

  it("attaches charts to their slide through absolute or relative targets", () => {
    const deck = zipSync({
      "ppt/slides/slide1.xml": strToU8('<p:sld><a:p><a:r><a:t>Result</a:t></a:r></a:p><a:p><a:r><a:t>Out of 405</a:t></a:r></a:p></p:sld>'),
      "ppt/slides/_rels/slide1.xml.rels": strToU8('<Relationships><Relationship Target="/ppt/charts/chart1.xml"/><Relationship Target="/ppt/notesSlides/notesSlide1.xml"/></Relationships>'),
      "ppt/charts/chart1.xml": strToU8(chart),
      "ppt/notesSlides/notesSlide1.xml": strToU8("<p:notes><a:p><a:r><a:t>Say this.</a:t></a:r></a:p></p:notes>"),
    });
    expect(chunkPptx("d.pptx", deck)[0]!.paragraphs).toEqual([
      "Out of 405",
      "Chart: Lower is better. Silent loss (%): Pattern list 41.2, Algorithm 0.",
      "Speaker notes: Say this.",
    ]);
  });
});
