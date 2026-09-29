// Shape of the corpus that `npm run index` writes and the API function ships with.

export interface Chunk {
  /** Stable id: `<path>#<anchor>` plus a part suffix when a section was split. */
  id: string;
  /** Repository-relative file path, e.g. `docs/safety-model.md`. */
  path: string;
  /** Human-readable location: document title, then section heading. */
  title: string;
  /** Heading anchor for deep links; empty for text before the first heading. */
  anchor: string;
  /** Paragraph-sized pieces. Claude cites whole pieces, so smaller is finer. */
  paragraphs: string[];
}

export interface Corpus {
  /** Commit the corpus was built from. */
  commit: string;
  /** True when a source file had uncommitted changes at build time. */
  dirty: boolean;
  /** ISO timestamp of the build. */
  builtAt: string;
  /** `https://github.com/<owner>/<repo>` when the origin remote is on GitHub. */
  repoUrl: string | null;
  chunks: Chunk[];
}

/** Link to a chunk's section in the repository at the corpus commit. */
export function chunkUrl(corpus: Corpus, chunk: Chunk): string | null {
  if (!corpus.repoUrl) return null;
  const path = chunk.path.split("/").map(encodeURIComponent).join("/");
  const anchor = chunk.anchor ? `#${chunk.anchor}` : "";
  return `${corpus.repoUrl}/blob/${corpus.commit}/${path}${anchor}`;
}
