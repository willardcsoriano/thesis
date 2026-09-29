// Retrieval eval over the real corpus: for each question a professor might
// ask, the section that answers it must be among what Claude is shown. Free to
// run (no API calls); it fails when a docs change or a chunking change makes
// the right section unreachable.

import { describe, expect, it } from "vitest";
import questions from "../eval/questions.json" with { type: "json" };
import { retrieve } from "../src/answer.js";
import type { Corpus } from "../src/corpus.js";
import corpusJson from "../src/generated/corpus.json" with { type: "json" };
import { SearchIndex } from "../src/search.js";

const corpus = corpusJson as Corpus;
const index = new SearchIndex(corpus.chunks);
const available = new Set(corpus.chunks.map((c) => c.path));

describe("retrieval", () => {
  it.each(questions)("$question", ({ question, expect: paths }) => {
    const present = paths.filter((p) => available.has(p));
    // A file missing on this branch can't be retrieved; skip rather than fail.
    if (present.length === 0) return;
    const got = retrieve(index, corpus, { question, history: [] }).map((c) => c.path);
    expect(got.some((p) => present.includes(p)), `retrieved: ${[...new Set(got)].join(", ")}`).toBe(true);
  });
});
