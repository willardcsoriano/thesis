import { describe, expect, it } from "vitest";
// @ts-expect-error: plain browser modules without type declarations
import { citeMarker } from "../public/render.js";
// @ts-expect-error: plain browser modules without type declarations
import { shareHtml, shareText } from "../public/share.js";

const qa = {
  question: "  Can every command be undone? ",
  mode: "quick",
  principle: `Nearly: it backs files up first.${citeMarker(1)}`,
  practice: `The analysis captures targets before running.${citeMarker(2)}${citeMarker(1)}`,
  sources: [
    { ref: 2, title: "Algorithms › Round 7", path: "docs/algorithms.md", url: "https://github.com/x/t/blob/abc/docs/algorithms.md#round-7" },
    { ref: 1, title: "Safety <model>", path: "docs/safety-model.md", url: null },
  ],
  commit: "0123456789abcdef",
};

describe("shareText", () => {
  it("lays out the question, both parts, numbered sources, and provenance", () => {
    expect(shareText(qa)).toBe(
      [
        "Q: Can every command be undone?",
        "",
        "In principle: Nearly: it backs files up first.[1]",
        "",
        "In practice: The analysis captures targets before running.[2][1]",
        "",
        "Sources:",
        "[1] Safety <model> (docs/safety-model.md)",
        "[2] Algorithms › Round 7 — https://github.com/x/t/blob/abc/docs/algorithms.md#round-7",
        "",
        "— SynapseBot · thesis at 0123456",
      ].join("\n"),
    );
  });

  it("gives a one-part answer without labels and marks study mode", () => {
    const text = shareText({ ...qa, mode: "study", practice: "", sources: [] });
    expect(text).toBe("Q: Can every command be undone?\n\nNearly: it backs files up first.[1]\n\n— SynapseBot (study) · thesis at 0123456");
  });
});

describe("shareHtml", () => {
  it("escapes everything and links sources", () => {
    const html = shareHtml(qa);
    expect(html).toContain("<p><strong>Q: Can every command be undone?</strong></p>");
    expect(html).toContain("<li>Safety &lt;model&gt; <code>docs/safety-model.md</code></li>");
    expect(html).toContain('<a href="https://github.com/x/t/blob/abc/docs/algorithms.md#round-7">Algorithms › Round 7</a>');
    expect(html).not.toContain("<sup>"); // no in-page footnote links in a pasted copy
  });
});
