// Answer eval: runs every question in questions.json through the real
// pipeline and Claude, then reports whether the answer cited an expected file.
// It calls the paid API (roughly $0.02-0.03 per question on Sonnet 5.5), so it
// only runs with --yes. Needs ANTHROPIC_API_KEY in the environment.
//
//   npm run eval -- --yes            all questions
//   npm run eval -- --yes --only 3   one question, by 1-based number
//
// Answers are written to eval/results/ (git-ignored) for reading.

import Anthropic from "@anthropic-ai/sdk";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { answer, type AnswerLog } from "../src/answer";
import type { Corpus } from "../src/corpus";
import corpusJson from "../src/generated/corpus.json";
import { SearchIndex } from "../src/search";
import questions from "./questions.json";

const { values } = parseArgs({ options: { yes: { type: "boolean" }, only: { type: "string" } } });
if (!values.yes) {
  console.error(`This calls the Claude API for ${questions.length} questions and costs money. Re-run with --yes.`);
  process.exit(2);
}

const corpus = corpusJson as Corpus;
const index = new SearchIndex(corpus.chunks);
const client = new Anthropic();
const open = (params: Parameters<Anthropic["beta"]["messages"]["stream"]>[0]) => client.beta.messages.stream(params);

const selected = values.only ? [questions[Number(values.only) - 1]!] : questions;
const report: string[] = [];
let passed = 0;
let inputTokens = 0;
let outputTokens = 0;

for (const { question, expect } of selected) {
  const log: AnswerLog = { retrieved: 0, cited: [], stopReason: null, inputTokens: 0, outputTokens: 0 };
  let text = "";
  for await (const event of answer(open, index, corpus, { question, history: [] }, log)) {
    if (event.type === "text") text += event.text;
    if (event.type === "cite") text += `[${event.ref}]`;
    if (event.type === "error" || event.type === "notice") text += `\n(${event.type}: ${event.text})`;
  }
  const citedPaths = log.cited.map((id) => id.split("#")[0]!);
  const ok = citedPaths.some((p) => expect.includes(p));
  if (ok) passed++;
  inputTokens += log.inputTokens;
  outputTokens += log.outputTokens;
  console.log(`${ok ? "PASS" : "MISS"}  ${question}\n      cited: ${log.cited.join(", ") || "(nothing)"}`);
  report.push(`## ${ok ? "PASS" : "MISS"}: ${question}\n\n${text}\n\nCited: ${log.cited.join(", ") || "(nothing)"}\n`);
}

const cost = (inputTokens * 2 + outputTokens * 10) / 1e6;
console.log(`\n${passed}/${selected.length} cited an expected source. ${inputTokens} in / ${outputTokens} out tokens, about $${cost.toFixed(2)}.`);

const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "results");
mkdirSync(outDir, { recursive: true });
const outFile = join(outDir, `${new Date().toISOString().replace(/[:.]/g, "-")}.md`);
writeFileSync(outFile, `## Overview\n\nAnswer eval at ${corpus.commit.slice(0, 7)}: ${passed}/${selected.length} cited an expected source.\n\n${report.join("\n")}`);
console.log(`Answers: ${outFile}`);
