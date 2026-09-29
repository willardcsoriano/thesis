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
