import { describe, expect, it } from "vitest";
import { MemoryRateLimiter } from "../src/rate-limit.js";

describe("MemoryRateLimiter", () => {
  it("allows up to the limit per window, per key, then resets", () => {
    let now = 0;
    const limiter = new MemoryRateLimiter(2, 1000, () => now);
    expect([limiter.allow("a"), limiter.allow("a"), limiter.allow("a")]).toEqual([true, true, false]);
    expect(limiter.allow("b")).toBe(true);
    now = 1000;
    expect(limiter.allow("a")).toBe(true);
  });

  it("forgets expired windows", () => {
    let now = 0;
    const limiter = new MemoryRateLimiter(1, 1000, () => now);
    limiter.allow("a");
    now = 5000;
    limiter.allow("b");
    expect((limiter as unknown as { windows: Map<string, unknown> }).windows.has("a")).toBe(false);
  });
});
