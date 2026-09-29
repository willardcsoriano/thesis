// Per-visitor fixed-window rate limit, held in memory.
//
// On Vercel each function instance has its own counters, and Fluid compute
// keeps instances warm, so this caps a single visitor well in practice but is
// not a global guarantee. The Anthropic workspace spend limit is the hard cap
// behind it. Counters hold IP addresses only until their window ends.

export interface RateLimiter {
  /** Records one request for `key`; false when the key is over its limit. */
  allow(key: string): boolean;
}

export class MemoryRateLimiter implements RateLimiter {
  private readonly windows = new Map<string, { count: number; resetAt: number }>();

  constructor(
    private readonly limit: number,
    private readonly windowMs: number,
    private readonly now: () => number = Date.now,
  ) {}

  allow(key: string): boolean {
    const now = this.now();
    this.sweep(now);
    const window = this.windows.get(key);
    if (!window || now >= window.resetAt) {
      this.windows.set(key, { count: 1, resetAt: now + this.windowMs });
      return true;
    }
    if (window.count >= this.limit) return false;
    window.count++;
    return true;
  }

  /** Drops expired windows so the map, and the IPs in it, don't accumulate. */
  private sweep(now: number): void {
    for (const [key, window] of this.windows) {
      if (now >= window.resetAt) this.windows.delete(key);
    }
  }
}
