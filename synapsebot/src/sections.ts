// Splits a streamed answer at the marker Claude writes between its
// "In principle" and "In practice" parts.
//
// Text arrives in arbitrary fragments, so a fragment ending in the start of
// the marker is held back until the next one shows whether the marker
// completes. Only the first marker splits; the answer has two parts.

export const PRACTICE_MARKER = "[[practice]]";

export type Piece = { type: "text"; text: string } | { type: "practice" };

export class SectionSplitter {
  private held = "";
  private split = false;

  /** Feeds one fragment; returns what can be shown now. */
  push(fragment: string): Piece[] {
    if (this.split) return text(fragment);
    const buffer = this.held + fragment;
    const at = buffer.indexOf(PRACTICE_MARKER);
    if (at >= 0) {
      this.split = true;
      this.held = "";
      return [
        ...text(buffer.slice(0, at).trimEnd()),
        { type: "practice" },
        ...text(buffer.slice(at + PRACTICE_MARKER.length).trimStart()),
      ];
    }
    const keep = heldLength(buffer);
    this.held = buffer.slice(buffer.length - keep);
    return text(buffer.slice(0, buffer.length - keep));
  }

  /** Releases held-back text: the stream ended or moved to a new block. */
  flush(): Piece[] {
    const rest = this.held;
    this.held = "";
    return text(rest);
  }
}

function text(value: string): Piece[] {
  return value ? [{ type: "text", text: value }] : [];
}

/**
 * How much of the end of `buffer` to hold back: a possible start of the
 * marker, plus the whitespace before it, which is trimmed if the marker
 * completes. Holding whitespace only delays it; nothing visible waits.
 */
function heldLength(buffer: string): number {
  let partial = 0;
  for (let n = Math.min(buffer.length, PRACTICE_MARKER.length - 1); n > 0; n--) {
    if (PRACTICE_MARKER.startsWith(buffer.slice(-n))) {
      partial = n;
      break;
    }
  }
  const before = buffer.slice(0, buffer.length - partial);
  return partial + (before.length - before.trimEnd().length);
}
