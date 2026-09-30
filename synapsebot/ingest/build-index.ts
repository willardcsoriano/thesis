// Builds src/generated/corpus.json from the files sources.txt allows.
//
// Run from synapsebot/ with `npm run index`. The build refuses to write a
// corpus that contains anything shaped like an email address or an API key:
// the page is public to anyone with the link and the access code, so a leak
// here is a publication.

import { execFileSync } from "node:child_process";
import { globSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import type { Chunk, Corpus } from "../src/corpus.js";
import { chunkHtml, chunkMarkdown } from "./chunk.js";

const here = dirname(fileURLToPath(import.meta.url));
const botDir = join(here, "..");
const repoRoot = join(botDir, "..");
const outFile = join(botDir, "src", "generated", "corpus.json");

/** Patterns that must never be published. Each hit fails the build. */
const FORBIDDEN: Array<[string, RegExp]> = [
  ["email address", /[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}/],
  ["Anthropic API key", /sk-ant-[A-Za-z0-9_-]{10,}/],
  ["private key", /-----BEGIN [A-Z ]*PRIVATE KEY-----/],
];

export function readAllowlist(text: string): { include: string[]; exclude: string[] } {
  const include: string[] = [];
  const exclude: string[] = [];
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    if (line.startsWith("!")) exclude.push(line.slice(1));
    else include.push(line);
  }
  return { include, exclude };
}

function resolveSources(): string[] {
  const { include, exclude } = readAllowlist(readFileSync(join(botDir, "sources.txt"), "utf8"));
  const excluded = new Set(exclude.flatMap((g) => globSync(g, { cwd: repoRoot })));
  const files = new Set<string>();
  for (const pattern of include) {
    const matches = globSync(pattern, { cwd: repoRoot });
    // A warning, not an error: branches differ in which files exist.
    if (matches.length === 0) console.warn(`warning: sources.txt: "${pattern}" matches no files`);
    for (const m of matches) if (!excluded.has(m)) files.add(m);
  }
  if (files.size === 0) throw new Error("sources.txt matches no files");
  return [...files].sort();
}

function git(...args: string[]): string {
  return execFileSync("git", args, { cwd: repoRoot, encoding: "utf8" }).trim();
}

/** Maps an origin remote to its https GitHub URL; other hosts get no links. */
export function githubUrl(remote: string): string | null {
  const m = /github\.com[:/]([^/]+)\/(.+?)(?:\.git)?$/.exec(remote.trim());
  return m ? `https://github.com/${m[1]}/${m[2]}` : null;
}

function originUrl(): string | null {
  try {
    return githubUrl(git("remote", "get-url", "origin"));
  } catch {
    return null;
  }
}

function checkPublishable(chunks: Chunk[]): void {
  const problems: string[] = [];
  for (const chunk of chunks) {
    const text = [chunk.title, ...chunk.paragraphs].join("\n");
    for (const [label, pattern] of FORBIDDEN) {
      if (pattern.test(text)) problems.push(`${chunk.id}: contains an ${label}`);
    }
  }
  if (problems.length) {
    throw new Error(
      "Refusing to build a corpus with private data. Remove it from the source " +
        "or drop the file from sources.txt:\n  " + problems.join("\n  "),
    );
  }
}

function main(): void {
  const files = resolveSources();
  const chunks = files.flatMap((file) => {
    const text = readFileSync(join(repoRoot, file), "utf8");
    return file.endsWith(".html") ? chunkHtml(file, text) : chunkMarkdown(file, text);
  });
  checkPublishable(chunks);

  const corpus: Corpus = {
    commit: git("rev-parse", "HEAD"),
    dirty: git("status", "--porcelain", "--", ...files) !== "",
    builtAt: new Date().toISOString(),
    repoUrl: originUrl(),
    chunks,
  };
  mkdirSync(dirname(outFile), { recursive: true });
  writeFileSync(outFile, JSON.stringify(corpus));

  const chars = chunks.reduce((n, c) => n + c.paragraphs.join("").length, 0);
  console.log(
    `corpus: ${files.length} files, ${chunks.length} chunks, ${(chars / 1024).toFixed(0)} KiB of text` +
      ` at ${corpus.commit.slice(0, 7)}${corpus.dirty ? " (uncommitted changes)" : ""}` +
      ` -> ${relative(process.cwd(), outFile)}`,
  );
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) main();
