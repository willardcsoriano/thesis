// Validates the browser's request body. Everything here comes from an
// anonymous visitor, so limits are tight and anything unexpected is rejected.

import { MODES, type Mode } from "./prompt.js";

export const MAX_QUESTION_CHARS = 1000;
export const MAX_HISTORY_TURNS = 6;
export const MAX_TURN_CHARS = 6000;
export const MAX_BODY_BYTES = 64 * 1024;

export interface Turn {
  role: "user" | "assistant";
  text: string;
}

export interface AskRequest {
  question: string;
  /** Answer style; absent means "quick". */
  mode: Mode;
  /** Earlier turns, oldest first, starting with a user turn and alternating. */
  history: Turn[];
}

export type Parsed = { ok: true; value: AskRequest } | { ok: false; error: string };

export function parseAskRequest(body: unknown): Parsed {
  if (typeof body !== "object" || body === null) return fail("Body must be a JSON object.");
  const { question, history = [], mode = "quick" } = body as Record<string, unknown>;
  if (!MODES.includes(mode as Mode)) return fail(`\`mode\` must be one of: ${MODES.join(", ")}.`);

  if (typeof question !== "string") return fail("`question` must be a string.");
  const q = question.trim();
  if (!q) return fail("`question` is empty.");
  if (q.length > MAX_QUESTION_CHARS) return fail(`Questions are limited to ${MAX_QUESTION_CHARS} characters.`);

  if (!Array.isArray(history)) return fail("`history` must be an array.");
  if (history.length % 2 !== 0) return fail("`history` must hold complete question-and-answer pairs.");
  // Keep only the most recent pairs (MAX_HISTORY_TURNS is even, so the cut starts on a user turn).
  const recent = history.slice(-MAX_HISTORY_TURNS);

  const turns: Turn[] = [];
  for (const [i, turn] of recent.entries()) {
    if (typeof turn !== "object" || turn === null) return fail("Each history turn must be an object.");
    const { role, text } = turn as Record<string, unknown>;
    const expected = i % 2 === 0 ? "user" : "assistant";
    if (role !== expected) return fail("`history` must alternate user and assistant turns, starting with user.");
    if (typeof text !== "string" || !text.trim()) return fail("Each history turn needs non-empty `text`.");
    turns.push({ role: expected, text: text.slice(0, MAX_TURN_CHARS) });
  }
  return { ok: true, value: { question: q, history: turns, mode: mode as Mode } };
}

function fail(error: string): Parsed {
  return { ok: false, error };
}
