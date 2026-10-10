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
      "<table><thead><tr><th>Approach</th><th>Silent loss</th></tr></thead>" +
        "<tbody><tr><td>Pattern list</td><td>41.2%</td></tr><tr><td>&lt;b&gt;x&lt;/b&gt;</td><td>0%</td></tr></tbody></table>",
    );
  });

  it("keeps a text diagram verbatim in a code block", () => {
    expect(renderAnswer("```\n[command] --> [analysis] --> backup | ask\n```", "e")).toBe(
      "<pre><code>[command] --&gt; [analysis] --&gt; backup | ask</code></pre>",
    );
  });

  it("ends a table at the first non-table line", () => {
    expect(renderAnswer("| a |\n| b |\nAfter.", "e")).toBe("<table><tbody><tr><td>a</td></tr><tr><td>b</td></tr></tbody></table><p>After.</p>");
  });
});
