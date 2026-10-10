import Anthropic from "@anthropic-ai/sdk";
import { describe, expect, it, vi } from "vitest";
import type { AnswerEvent, AnswerStream, OpenStream, StreamParams } from "../src/answer.js";
import { NOTHING_FOUND } from "../src/answer.js";
import type { Corpus } from "../src/corpus.js";
import { clientIp, createApp, type Env } from "../src/app.js";
import { MemoryRateLimiter } from "../src/rate-limit.js";

const corpus: Corpus = {
  commit: "abcdef1234567890",
  dirty: false,
  builtAt: "2026-09-29T00:00:00.000Z",
  repoUrl: "https://github.com/example/thesis",
  chunks: [
    { id: "README.md#overview", path: "README.md", title: "Readme › Overview", anchor: "overview", paragraphs: ["SynapseOS turns requests into commands."] },
    { id: "docs/safety-model.md#undo", path: "docs/safety-model.md", title: "Safety › Undo", anchor: "undo", paragraphs: ["Every mutating command records an undo path."] },
  ],
};

type Event = Anthropic.Beta.BetaRawMessageStreamEvent;

/** A fake Claude stream that replays the given events and final message. */
function fakeStream(events: Event[], stopReason: Anthropic.Beta.BetaMessage["stop_reason"] = "end_turn"): AnswerStream {
  return {
    async *[Symbol.asyncIterator]() {
      yield* events;
    },
    finalMessage: async () =>
      ({ stop_reason: stopReason, usage: { input_tokens: 100, output_tokens: 20 } }) as Anthropic.Beta.BetaMessage,
  };
}

const text = (t: string): Event => ({ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: t } });
const cite = (i: number): Event =>
  ({
    type: "content_block_delta",
    index: 0,
    delta: {
      type: "citations_delta",
      citation: { type: "search_result_location", search_result_index: i, source: "", title: "", cited_text: "", start_block_index: 0, end_block_index: 1 },
    },
  }) as Event;
const stop: Event = { type: "content_block_stop", index: 0 };

const env: Env = { ANTHROPIC_API_KEY: "test-key", ACCESS_CODE: "open sesame" };

function setup(open: OpenStream, rateLimiter?: MemoryRateLimiter) {
  const spy = vi.fn(open);
  return { app: createApp({ corpus, openStream: () => spy, rateLimiter }), spy };
}

function ask(body: unknown, code = "open sesame"): Request {
  return new Request("https://bot.test/api/ask", {
    method: "POST",
    headers: { "content-type": "application/json", "x-access-code": code, "x-vercel-forwarded-for": "203.0.113.9" },
    body: JSON.stringify(body),
  });
}

async function events(res: Response): Promise<AnswerEvent[]> {
  const body = await res.text();
  return body
    .split("\n\n")
    .filter(Boolean)
    .map((frame) => JSON.parse(frame.replace(/^data: /, "")) as AnswerEvent);
}

describe("POST /api/ask", () => {
  it("streams text with numbered, linked sources", async () => {
    const { app, spy } = setup(() => fakeStream([text("Undo is recorded"), cite(1), text("."), stop]));
    const res = await app.fetch(ask({ question: "how does undo work?" }), env);

    expect(res.headers.get("content-type")).toContain("text/event-stream");
    expect(await events(res)).toEqual([
      { type: "text", text: "Undo is recorded" },
      {
        type: "source",
        ref: 1,
        path: "docs/safety-model.md",
        title: "Safety › Undo",
        url: "https://github.com/example/thesis/blob/abcdef1234567890/docs/safety-model.md#undo",
      },
      { type: "text", text: "." },
      { type: "cite", ref: 1 },
      { type: "done" },
    ]);

    const params = spy.mock.calls[0]![0] as StreamParams;
    expect(params.model).toBe("claude-sonnet-5-5");
    expect(params.fallbacks).toBe("default");
    const last = params.messages.at(-1)!;
    const blocks = last.content as Anthropic.Beta.BetaContentBlockParam[];
    // The orientation section rides along with the hit.
    expect(blocks.filter((b) => b.type === "search_result").map((b) => b.source)).toEqual([
      "README.md",
      "docs/safety-model.md",
    ]);
  });

  it("marks where the In practice part begins", async () => {
    const { app } = setup(() =>
      fakeStream([text("Every change can be reversed.\n[[prac"), text("tice]]\nUndo is recorded"), cite(1), text("."), stop]),
    );
    const got = await events(await app.fetch(ask({ question: "how does undo work?" }), env));
    expect(got.filter((e) => e.type !== "source")).toEqual([
      { type: "text", text: "Every change can be reversed." },
      { type: "practice" },
      { type: "text", text: "Undo is recorded" },
      { type: "text", text: "." },
      { type: "cite", ref: 1 },
      { type: "done" },
    ]);
  });

  it("uses each mode's settings for the Claude call", async () => {
    const { app, spy } = setup(() => fakeStream([text("ok"), stop]));
    await (await app.fetch(ask({ question: "how does undo work?" }), env)).text();
    await (await app.fetch(ask({ question: "how does undo work?", mode: "study" }), env)).text();
    const [quick, study] = spy.mock.calls.map((c) => c[0] as StreamParams);
    expect([quick!.max_tokens, quick!.output_config?.effort]).toEqual([2000, "low"]);
    expect([study!.max_tokens, study!.output_config?.effort]).toEqual([6000, "medium"]);
    expect(String(study!.system)).toContain("study mode");
    expect(String(quick!.system)).not.toContain("study mode");
  });

  it("answers without calling Claude when nothing in the corpus matches", async () => {
    const { app, spy } = setup(() => fakeStream([]));
    const res = await app.fetch(ask({ question: "hello there" }), env);
    expect(await events(res)).toEqual([{ type: "text", text: NOTHING_FOUND }, { type: "done" }]);
    expect(spy).not.toHaveBeenCalled();
  });

  it("withdraws the partial answer on a refusal", async () => {
    const { app } = setup(() => fakeStream([text("partial"), stop], "refusal"));
    const got = await events(await app.fetch(ask({ question: "undo command" }), env));
    expect(got.map((e) => e.type)).toEqual(["text", "reset", "notice", "done"]);
  });

  it("reports upstream failures without leaking their text", async () => {
    const { app } = setup(() => {
      throw new Anthropic.InternalServerError(500, { secret: "upstream detail" }, "upstream detail", new Headers());
    });
    const got = await events(await app.fetch(ask({ question: "undo command" }), env));
    expect(got).toEqual([
      { type: "error", text: "The model service is temporarily unavailable. Please try again shortly." },
      { type: "done" },
    ]);
  });

  it("refuses when unconfigured, and on a wrong code", async () => {
    const { app, spy } = setup(() => fakeStream([]));
    expect((await app.fetch(ask({ question: "undo" }), {})).status).toBe(503);
    expect((await app.fetch(ask({ question: "undo" }, "guess"), env)).status).toBe(401);
    expect(spy).not.toHaveBeenCalled();
  });

  it("rate-limits per visitor", async () => {
    const { app } = setup(() => fakeStream([]), new MemoryRateLimiter(1, 60_000));
    expect((await app.fetch(ask({ question: "hello" }), env)).status).toBe(200);
    expect((await app.fetch(ask({ question: "hello" }), env)).status).toBe(429);
  });

  it("rejects malformed bodies", async () => {
    const { app } = setup(() => fakeStream([]));
    expect((await app.fetch(ask({ question: "" }), env)).status).toBe(400);
    const bad = new Request("https://bot.test/api/ask", {
      method: "POST",
      headers: { "x-access-code": "open sesame" },
      body: "{not json",
    });
    expect((await app.fetch(bad, env)).status).toBe(400);
  });
});

describe("other routes", () => {
  it("serves build metadata and 404s the rest", async () => {
    const { app } = setup(() => fakeStream([]));
    const meta = await app.fetch(new Request("https://bot.test/api/meta"), env);
    expect(await meta.json()).toMatchObject({ commit: corpus.commit, model: "claude-sonnet-5-5" });
    expect((await app.fetch(new Request("https://bot.test/api/nope"), env)).status).toBe(404);
    expect((await app.fetch(new Request("https://bot.test/api/ask"), env)).status).toBe(405);
  });
});

describe("clientIp", () => {
  it("prefers Vercel's header, then the first forwarded address", () => {
    expect(clientIp(new Headers({ "x-vercel-forwarded-for": "1.1.1.1", "x-forwarded-for": "2.2.2.2" }))).toBe("1.1.1.1");
    expect(clientIp(new Headers({ "x-forwarded-for": "2.2.2.2, 10.0.0.1" }))).toBe("2.2.2.2");
    expect(clientIp(new Headers())).toBe("unknown");
  });
});
