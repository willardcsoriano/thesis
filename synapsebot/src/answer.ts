// Runs one question through retrieval and Claude and yields the events the
// page renders. Kept free of HTTP so tests and the eval can drive it directly.

import Anthropic from "@anthropic-ai/sdk";
import { chunkUrl, type Chunk, type Corpus } from "./corpus";
import { buildMessages, MAX_TOKENS, MODEL, SYSTEM_PROMPT } from "./prompt";
import type { AskRequest } from "./request";
import type { SearchIndex } from "./search";

/** Most sections sent per question, and the character budget they share. */
export const MAX_RESULTS = 8;
export const RESULT_CHAR_BUDGET = 24_000;
/** Sent with every answered question so broad questions get the project summary. First match wins. */
export const ORIENTATION_IDS = ["docs/vision.md#overview", "README.md#overview"];

export type StreamParams = Parameters<Anthropic["beta"]["messages"]["stream"]>[0];
export interface AnswerStream extends AsyncIterable<Anthropic.Beta.BetaRawMessageStreamEvent> {
  finalMessage(): Promise<Anthropic.Beta.BetaMessage>;
}
export type OpenStream = (params: StreamParams) => AnswerStream;

export type AnswerEvent =
  | { type: "text"; text: string }
  /** First citation of a section: its footnote number and where it lives. */
  | { type: "source"; ref: number; path: string; title: string; url: string | null }
  /** A footnote marker at the end of the text written so far. */
  | { type: "cite"; ref: number }
  /** Discard the text so far (the answer was withdrawn mid-stream). */
  | { type: "reset" }
  | { type: "notice"; text: string }
  | { type: "error"; text: string }
  | { type: "done" };

/** Summary for the operator log: no question text, no visitor data. */
export interface AnswerLog {
  retrieved: number;
  cited: string[];
  stopReason: string | null;
  inputTokens: number;
  outputTokens: number;
  error?: string;
}

export const NOTHING_FOUND =
  "I couldn't find anything in the thesis materials about that. Try asking about " +
  "SynapseOS itself: what it is, how its safety model works, how the study is designed, " +
  "or what the prototype does today.";

export function retrieve(index: SearchIndex, corpus: Corpus, request: AskRequest): Chunk[] {
  const previousQuestion = request.history.findLast((t) => t.role === "user")?.text ?? "";
  const hits = index.search(request.question, MAX_RESULTS, previousQuestion);
  if (hits.length === 0) return [];

  const chunks: Chunk[] = [];
  let budget = RESULT_CHAR_BUDGET;
  const orientation = ORIENTATION_IDS.map((id) => corpus.chunks.find((c) => c.id === id)).find(Boolean);
  for (const chunk of [orientation, ...hits.map((h) => h.chunk)]) {
    if (!chunk || chunks.includes(chunk)) continue;
    const size = chunk.paragraphs.join("").length;
    if (size > budget && chunks.length > 0) continue;
    chunks.push(chunk);
    budget -= size;
  }
  return chunks;
}

export async function* answer(
  openStream: OpenStream,
  index: SearchIndex,
  corpus: Corpus,
  request: AskRequest,
  log: AnswerLog,
): AsyncGenerator<AnswerEvent> {
  const chunks = retrieve(index, corpus, request);
  log.retrieved = chunks.length;
  if (chunks.length === 0) {
    yield { type: "text", text: NOTHING_FOUND };
    yield { type: "done" };
    return;
  }

  const refs = new Map<number, number>(); // search_result_index -> footnote number
  let pending: number[] = []; // footnotes cited by the text block being streamed

  try {
    const stream = openStream({
      model: MODEL,
      max_tokens: MAX_TOKENS,
      system: SYSTEM_PROMPT,
      thinking: { type: "adaptive" },
      output_config: { effort: "low" },
      // On a safety-classifier decline, retry server-side on the model
      // Anthropic recommends for that refusal category.
      betas: ["server-side-fallback-2026-07-01"],
      fallbacks: "default",
      messages: buildMessages(corpus, request, chunks),
    });

    for await (const event of stream) {
      if (event.type === "content_block_delta") {
        const delta = event.delta;
        if (delta.type === "text_delta") {
          yield { type: "text", text: delta.text };
        } else if (delta.type === "citations_delta" && delta.citation.type === "search_result_location") {
          const index = delta.citation.search_result_index;
          const chunk = chunks[index];
          if (!chunk) continue;
          let ref = refs.get(index);
          if (ref === undefined) {
            ref = refs.size + 1;
            refs.set(index, ref);
            log.cited.push(chunk.id);
            yield { type: "source", ref, path: chunk.path, title: chunk.title, url: chunkUrl(corpus, chunk) };
          }
          if (!pending.includes(ref)) pending.push(ref);
        }
      } else if (event.type === "content_block_stop") {
        for (const ref of pending) yield { type: "cite", ref };
        pending = [];
      }
    }

    const final = await stream.finalMessage();
    log.stopReason = final.stop_reason;
    log.inputTokens = final.usage.input_tokens;
    log.outputTokens = final.usage.output_tokens;
    if (final.stop_reason === "refusal") {
      yield { type: "reset" };
      yield {
        type: "notice",
        text: "This question was declined by the model's safety checks. Try rephrasing it, or ask the author directly.",
      };
    } else if (final.stop_reason === "max_tokens") {
      yield { type: "notice", text: "The answer was cut off at its length limit. Ask a narrower follow-up for the rest." };
    }
  } catch (err) {
    log.error = describeError(err);
    yield { type: "error", text: visitorMessage(err) };
  }
  yield { type: "done" };
}

function describeError(err: unknown): string {
  if (err instanceof Anthropic.APIError) return `api ${err.status ?? "connection"}`;
  return err instanceof Error ? err.name : "unknown";
}

/** What the visitor sees: actionable, never the upstream error text. */
function visitorMessage(err: unknown): string {
  if (err instanceof Anthropic.RateLimitError) return "SynapseBot is busy right now. Please try again in a minute.";
  if (err instanceof Anthropic.APIError && (err.status === 529 || (err.status ?? 0) >= 500)) {
    return "The model service is temporarily unavailable. Please try again shortly.";
  }
  return "Something went wrong while answering. Please try again; if it keeps happening, tell the author.";
}
