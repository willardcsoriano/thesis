import { describe, expect, it } from "vitest";
import { PRACTICE_MARKER, SectionSplitter, type Piece } from "../src/sections.js";

function run(fragments: string[]): Piece[] {
  const splitter = new SectionSplitter();
  const pieces = fragments.flatMap((f) => splitter.push(f));
  return [...pieces, ...splitter.flush()];
}

/** Joins adjacent text pieces so assertions don't depend on fragment boundaries. */
function merged(pieces: Piece[]): Array<string | "|"> {
  const out: Array<string | "|"> = [];
  for (const p of pieces) {
    if (p.type === "practice") out.push("|");
    else if (typeof out.at(-1) === "string" && out.at(-1) !== "|") out[out.length - 1] += p.text;
    else out.push(p.text);
  }
  return out;
}

describe("SectionSplitter", () => {
  it("splits at the marker and trims the whitespace around it", () => {
    expect(merged(run([`It asks first.\n${PRACTICE_MARKER}\nUnknown commands are unrecoverable.`]))).toEqual([
      "It asks first.",
      "|",
      "Unknown commands are unrecoverable.",
    ]);
  });

  it("finds a marker split across fragments without leaking any of it", () => {
    const pieces = run(["It asks.\n[[pra", "ct", "ice]]\nDetail."]);
    expect(merged(pieces)).toEqual(["It asks.", "|", "Detail."]);
    expect(pieces.some((p) => p.type === "text" && p.text.includes("[["))).toBe(false);
  });

  it("releases held text that turned out not to be the marker", () => {
    expect(merged(run(["see [[pr", "ior work"]))).toEqual(["see [[prior work"]);
  });

  it("passes an answer without a marker through unchanged", () => {
    expect(merged(run(["Not covered. ", "Ask the author."]))).toEqual(["Not covered. Ask the author."]);
  });

  it("splits only once", () => {
    expect(merged(run([`A${PRACTICE_MARKER}B${PRACTICE_MARKER}C`]))).toEqual(["A", "|", `B${PRACTICE_MARKER}C`]);
  });
});
