// Wires the app to its production dependencies: the bundled corpus, the
// Anthropic client, and the rate limiter. Shared by the Vercel function and
// the local dev server.

import Anthropic from "@anthropic-ai/sdk";
import corpusJson from "./generated/corpus.json" with { type: "json" };
import { createApp } from "./app.js";
import type { Corpus } from "./corpus.js";
import { MemoryRateLimiter } from "./rate-limit.js";

/** Questions per visitor per minute. */
export const QUESTIONS_PER_MINUTE = 8;

export const app = createApp({
  corpus: corpusJson as Corpus,
  openStream: (env) => {
    const client = new Anthropic({ apiKey: env.ANTHROPIC_API_KEY });
    return (params) => client.beta.messages.stream(params);
  },
  rateLimiter: new MemoryRateLimiter(QUESTIONS_PER_MINUTE, 60_000),
});
