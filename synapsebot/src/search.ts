// BM25 keyword search over the corpus chunks.
//
// The corpus is a few hundred chunks, so an in-memory index built once per
// Worker isolate is fast enough and needs no database or embedding service.
// A chunk's title is indexed twice so a question naming a section
// ("the safety model") favours that section.

import type { Chunk } from "./corpus";

const K1 = 1.2;
const B = 0.75;

const STOPWORDS = new Set(
  (
    "a an and are as at be but by can do does for from how i if in is it its of on or so " +
    "that the their then there these this to was what when where which who why will with " +
    "you your me my we our about into than them they has have had not no yes also just"
  ).split(" "),
);

/**
 * Crude suffix folding so "serves", "served" and "serving" meet at "serv".
 * Applied identically to documents and questions, so it only has to be consistent.
 */
export function stem(word: string): string {
  for (const suffix of ["ing", "ed", "es", "s"]) {
    if (word.endsWith(suffix) && word.length - suffix.length >= 4 && !word.endsWith("ss")) {
      return word.slice(0, -suffix.length);
    }
  }
  return word;
}

/** Lowercases, splits on non-alphanumerics, drops stopwords, and stems. */
export function tokenize(text: string): string[] {
  const tokens: string[] = [];
  for (const raw of text.toLowerCase().split(/[^\p{L}\p{N}]+/u)) {
    if (raw.length < 2 || STOPWORDS.has(raw)) continue;
    tokens.push(stem(raw));
  }
  return tokens;
}

export interface Hit {
  chunk: Chunk;
  score: number;
}

export class SearchIndex {
  private readonly termFreqs: Array<Map<string, number>>;
  private readonly lengths: number[];
  private readonly docFreq = new Map<string, number>();
  private readonly avgLength: number;

  constructor(private readonly chunks: readonly Chunk[]) {
    this.termFreqs = chunks.map((chunk) => {
      const counts = new Map<string, number>();
      const tokens = tokenize(`${chunk.title} ${chunk.title} ${chunk.paragraphs.join(" ")}`);
      for (const t of tokens) counts.set(t, (counts.get(t) ?? 0) + 1);
      for (const t of counts.keys()) this.docFreq.set(t, (this.docFreq.get(t) ?? 0) + 1);
      return counts;
    });
    this.lengths = this.termFreqs.map((m) => [...m.values()].reduce((a, b) => a + b, 0));
    this.avgLength = this.lengths.reduce((a, b) => a + b, 0) / Math.max(1, chunks.length);
  }

  /**
   * Returns up to `limit` chunks ranked by BM25. `context` (for example the
   * previous question, so "why?" follow-ups still find their topic) counts at
   * half weight.
   */
  search(query: string, limit: number, context = ""): Hit[] {
    const weights = new Map<string, number>();
    for (const t of tokenize(context)) weights.set(t, Math.max(weights.get(t) ?? 0, 0.5));
    for (const t of tokenize(query)) weights.set(t, 1);
    if (weights.size === 0) return [];

    const n = this.chunks.length;
    const hits: Hit[] = [];
    this.termFreqs.forEach((counts, i) => {
      let score = 0;
      for (const [term, weight] of weights) {
        const tf = counts.get(term);
        if (!tf) continue;
        const df = this.docFreq.get(term) ?? 0;
        const idf = Math.log(1 + (n - df + 0.5) / (df + 0.5));
        const norm = tf + K1 * (1 - B + (B * this.lengths[i]!) / this.avgLength);
        score += weight * idf * ((tf * (K1 + 1)) / norm);
      }
      if (score > 0) hits.push({ chunk: this.chunks[i]!, score });
    });
    return hits.sort((a, b) => b.score - a.score).slice(0, limit);
  }
}
