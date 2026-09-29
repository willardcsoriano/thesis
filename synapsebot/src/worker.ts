// Cloudflare Worker for SynapseBot. Static files in public/ are served by the
// platform; only /api/* reaches this handler (see wrangler.jsonc).
//
//   GET  /api/meta  what the corpus was built from (no auth; nothing private)
//   POST /api/ask   { question, history } -> text/event-stream of AnswerEvents
//
// Privacy: questions are sent to the Anthropic API to be answered and are not
// stored here. The log line per answer holds counts and cited section ids
// only, never the question text or anything about the visitor. The visitor's
// IP is used as the rate-limit key and nothing else.

import Anthropic from "@anthropic-ai/sdk";
import corpusJson from "./generated/corpus.json";
import { answer, type AnswerEvent, type AnswerLog, type OpenStream } from "./answer";
import type { Corpus } from "./corpus";
import { MODEL } from "./prompt";
import { MAX_BODY_BYTES, parseAskRequest } from "./request";
import { SearchIndex } from "./search";

/** The subset of Cloudflare's rate-limit binding this Worker uses. */
export interface RateLimiter {
  limit(options: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  ANTHROPIC_API_KEY?: string;
  /** Shared passphrase for the class. Unset means closed: every question is refused. */
  ACCESS_CODE?: string;
  RATE_LIMITER?: RateLimiter;
}

export interface Deps {
  corpus: Corpus;
  openStream: (env: Env) => OpenStream;
}

export function createApp({ corpus, openStream }: Deps) {
  const index = new SearchIndex(corpus.chunks);

  async function ask(request: Request, env: Env): Promise<Response> {
    if (!env.ACCESS_CODE || !env.ANTHROPIC_API_KEY) {
      return json({ error: "SynapseBot is not configured yet." }, 503);
    }
    if (!(await sameSecret(request.headers.get("x-access-code") ?? "", env.ACCESS_CODE))) {
      return json({ error: "That access code is not right." }, 401);
    }
    if (env.RATE_LIMITER) {
      const key = request.headers.get("cf-connecting-ip") ?? "unknown";
      const { success } = await env.RATE_LIMITER.limit({ key });
      if (!success) return json({ error: "Too many questions at once. Please wait a minute." }, 429);
    }

    const raw = await request.text();
    if (new TextEncoder().encode(raw).length > MAX_BODY_BYTES) return json({ error: "Request too large." }, 413);
    let body: unknown;
    try {
      body = JSON.parse(raw);
    } catch {
      return json({ error: "Body must be JSON." }, 400);
    }
    const parsed = parseAskRequest(body);
    if (!parsed.ok) return json({ error: parsed.error }, 400);

    const log: AnswerLog = { retrieved: 0, cited: [], stopReason: null, inputTokens: 0, outputTokens: 0 };
    const events = answer(openStream(env), index, corpus, parsed.value, log);
    return sse(events, () => console.log(JSON.stringify({ event: "answer", ...log })));
  }

  return {
    async fetch(request: Request, env: Env): Promise<Response> {
      const { pathname } = new URL(request.url);
      if (pathname === "/api/meta" && request.method === "GET") {
        return json({
          commit: corpus.commit,
          dirty: corpus.dirty,
          builtAt: corpus.builtAt,
          repoUrl: corpus.repoUrl,
          model: MODEL,
        });
      }
      if (pathname === "/api/ask") {
        if (request.method !== "POST") return json({ error: "Use POST." }, 405);
        return ask(request, env);
      }
      return json({ error: "Not found." }, 404);
    },
  };
}

function sse(events: AsyncGenerator<AnswerEvent>, onFinish: () => void): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    async pull(controller) {
      const next = await events.next();
      if (next.done) {
        onFinish();
        controller.close();
        return;
      }
      controller.enqueue(encoder.encode(`data: ${JSON.stringify(next.value)}\n\n`));
    },
    async cancel() {
      // The visitor closed the page: stop generating (and paying for) the answer.
      await events.return(undefined);
      onFinish();
    },
  });
  return new Response(body, {
    headers: {
      "content-type": "text/event-stream; charset=utf-8",
      "cache-control": "no-store",
      "x-content-type-options": "nosniff",
    },
  });
}

function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", "cache-control": "no-store" },
  });
}

/** Compares secrets in constant time by comparing fixed-length digests. */
async function sameSecret(given: string, expected: string): Promise<boolean> {
  const digest = async (s: string) =>
    new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s)));
  const [a, b] = await Promise.all([digest(given), digest(expected)]);
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i]! ^ b[i]!;
  return diff === 0;
}

export default createApp({
  corpus: corpusJson as Corpus,
  openStream: (env) => {
    const client = new Anthropic({ apiKey: env.ANTHROPIC_API_KEY });
    return (params) => client.beta.messages.stream(params);
  },
});
