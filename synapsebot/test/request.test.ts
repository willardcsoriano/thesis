import { describe, expect, it } from "vitest";
import { MAX_HISTORY_TURNS, MAX_QUESTION_CHARS, parseAskRequest } from "../src/request";

describe("parseAskRequest", () => {
  it("accepts a question with alternating history", () => {
    const r = parseAskRequest({
      question: "  Why?  ",
      history: [{ role: "user", text: "What is it?" }, { role: "assistant", text: "A thesis." }],
    });
    expect(r).toEqual({
      ok: true,
      value: { question: "Why?", history: [{ role: "user", text: "What is it?" }, { role: "assistant", text: "A thesis." }] },
    });
  });

  it.each([
    [null, "JSON object"],
    [{ question: 3 }, "must be a string"],
    [{ question: "  " }, "empty"],
    [{ question: "x".repeat(MAX_QUESTION_CHARS + 1) }, "limited"],
    [{ question: "q", history: "no" }, "array"],
    [{ question: "q", history: [{ role: "assistant", text: "a" }, { role: "user", text: "b" }] }, "alternate"],
    [{ question: "q", history: [{ role: "user", text: "" }, { role: "assistant", text: "b" }] }, "non-empty"],
  ])("rejects %j", (body, message) => {
    const r = parseAskRequest(body);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain(message);
  });

  it("rejects a history that ends on an unanswered question", () => {
    const r = parseAskRequest({ question: "q", history: [{ role: "user", text: "a" }] });
    expect(r.ok).toBe(false);
  });

  it("keeps only the most recent pairs", () => {
    const history = Array.from({ length: MAX_HISTORY_TURNS + 4 }, (_, i) => ({
      role: i % 2 === 0 ? "user" : "assistant",
      text: `t${i}`,
    }));
    const r = parseAskRequest({ question: "q", history });
    expect(r.ok && r.value.history[0]!.role).toBe("user");
    expect(r.ok && r.value.history.length).toBeLessThanOrEqual(MAX_HISTORY_TURNS);
    expect(r.ok && r.value.history.at(-1)!.text).toBe(`t${MAX_HISTORY_TURNS + 3}`);
  });
});
