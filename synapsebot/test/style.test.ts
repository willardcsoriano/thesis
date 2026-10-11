// Guards the stylesheet against the edit that once spliced the heading style
// into the answer-paragraph selector, turning every answer into small orange
// capitals.

import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../public/style.css", import.meta.url), "utf8");

/** Each rule's selector and body, ignoring comments; nested @media bodies are flattened. */
function rules(source: string): Array<{ selector: string; body: string }> {
  const flat = source.replace(/\/\*[\s\S]*?\*\//g, "");
  return [...flat.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({ selector: m[1]!.trim(), body: m[2]! }));
}

describe("style.css", () => {
  it("has balanced braces and no comment inside a selector list", () => {
    expect(css.split("{").length).toBe(css.split("}").length);
    expect(css).not.toMatch(/,\s*\/\*/);
  });

  it("never sets answer prose in the uppercase label style", () => {
    const prose = /(^|,)\s*\.answer (p|ul|ol|li)\b/;
    for (const { selector, body } of rules(css)) {
      if (prose.test(selector)) expect(body, selector).not.toMatch(/text-transform:\s*uppercase/);
    }
  });
});
