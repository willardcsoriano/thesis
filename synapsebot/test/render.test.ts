import { describe, expect, it } from "vitest";
// @ts-expect-error: plain browser module without type declarations
import { citeMarker, renderAnswer, stripCiteMarkers } from "../public/render.js";

describe("renderAnswer", () => {
  it("escapes HTML from the model", () => {
    const html = renderAnswer('<img src=x onerror="alert(1)"> and <script>x</script>', "e1");
    expect(html).not.toContain("<img");
    expect(html).not.toContain("<script");
    expect(html).toContain("&lt;img src=x onerror=&quot;alert(1)&quot;&gt;");
  });

  it("renders paragraphs, emphasis, code, and lists", () => {
    const html = renderAnswer("It **fails closed** and *asks*.\n\n- one `ls -la`\n- two\n\n1. first", "e1");
    expect(html).toBe(
      "<p>It <strong>fails closed</strong> and <em>asks</em>.</p>" +
        "<ul><li>one <code>ls -la</code></li><li>two</li></ul>" +
        "<ol><li>first</li></ol>",
    );
  });

  it("keeps code blocks verbatim and escaped", () => {
    expect(renderAnswer("```sh\nrm -rf <dir> **x**\n```", "e1")).toBe(
      "<pre><code>rm -rf &lt;dir&gt; **x**</code></pre>",
    );
  });

  it("turns cite markers into footnote links scoped to the entry", () => {
    const html = renderAnswer(`Undo is recorded.${citeMarker(2)}`, "entry-3");
    expect(html).toBe('<p>Undo is recorded.<sup><a href="#entry-3-fn-2" aria-label="Source 2">2</a></sup></p>');
    expect(stripCiteMarkers(`a${citeMarker(1)}b`)).toBe("ab");
  });

  it("leaves snake_case and file paths alone", () => {
    expect(renderAnswer("see build_index and docs/safety_model.md", "e")).toBe(
      "<p>see build_index and docs/safety_model.md</p>",
    );
  });
});

describe("study-mode structure", () => {
  it("renders small headings", () => {
    expect(renderAnswer("## How it works\nStep one.", "e")).toBe("<h3>How it works</h3><p>Step one.</p>");
  });

  it("renders a table with a header row, escaping cells", () => {
    expect(renderAnswer("| Approach | Silent loss |\n|---|---:|\n| Pattern list | 41.2% |\n| <b>x</b> | 0% |", "e")).toBe(
      '<div class="table-wrap"><table><thead><tr><th>Approach</th><th>Silent loss</th></tr></thead>' +
        "<tbody><tr><td>Pattern list</td><td>41.2%</td></tr><tr><td>&lt;b&gt;x&lt;/b&gt;</td><td>0%</td></tr></tbody></table></div>",
    );
  });

  it("keeps a text diagram verbatim in a code block", () => {
    expect(renderAnswer("```\n[command] --> [analysis] --> backup | ask\n```", "e")).toBe(
      "<pre><code>[command] --&gt; [analysis] --&gt; backup | ask</code></pre>",
    );
  });

  it("ends a table at the first non-table line", () => {
    expect(renderAnswer("| a |\n| b |\nAfter.", "e")).toBe(
      '<div class="table-wrap"><table><tbody><tr><td>a</td></tr><tr><td>b</td></tr></tbody></table></div><p>After.</p>',
    );
  });
});

describe("emphasis around code", () => {
  it("bolds a code span wrapped in **", () => {
    expect(renderAnswer("**`internal/effects/`** holds the analysis.", "e")).toBe(
      "<p><strong><code>internal/effects/</code></strong> holds the analysis.</p>",
    );
  });

  it("italicises a code span and still leaves asterisks inside code alone", () => {
    expect(renderAnswer("*`rm -rf *~`* is risky; `a**b**c` stays literal.", "e")).toBe(
      "<p><em><code>rm -rf *~</code></em> is risky; <code>a**b**c</code> stays literal.</p>",
    );
  });

  it("bolds a phrase that mixes text and code", () => {
    expect(renderAnswer("**the `gate` package** wires it in", "e")).toBe(
      "<p><strong>the <code>gate</code> package</strong> wires it in</p>",
    );
  });
});

describe("bold lines used as headings", () => {
  it("renders a bold-only line as a heading, alone or leading a paragraph", () => {
    expect(renderAnswer("**How the sorting works**\n\nThe analysis turns it into effects.", "e")).toBe(
      "<h3>How the sorting works</h3><p>The analysis turns it into effects.</p>",
    );
    expect(renderAnswer("**Don't oversell the verdict:**\nA simple list reaches the same verdict.", "e")).toBe(
      "<h3>Don&#39;t oversell the verdict</h3><p>A simple list reaches the same verdict.</p>",
    );
  });

  it("leaves bold inside a sentence alone", () => {
    expect(renderAnswer("**Bold start** of a sentence.", "e")).toBe("<p><strong>Bold start</strong> of a sentence.</p>");
  });
});
