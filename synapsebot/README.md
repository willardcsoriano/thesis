## Overview

SynapseBot is a public web page where classmates and professors ask questions about the SynapseOS thesis and get answers drawn only from the thesis's own documents, each claim footnoted with a link to the file and section it came from. It is hosted on Vercel as a static, book-styled page plus one Node function: it searches a keyword index of the allowlisted repository files and sends the best-matching sections to Claude Sonnet 5.5 as citable search results. The bot reads only what `sources.txt` lists, and the build refuses to publish anything shaped like an email address or API key. Access is gated by a shared class passphrase, a per-visitor rate limit, and your Anthropic spend cap. This directory is self-contained: it has its own Node toolchain and its own CI job, and neither touches the Go prototype.

## Table of Contents

- [Overview](#overview)
- [Layout](#layout)
- [How an answer is made](#how-an-answer-is-made)
- [Running it](#running-it)
  - [First deployment](#first-deployment)
- [Keeping it current](#keeping-it-current)
- [Privacy and cost](#privacy-and-cost)
- [Design direction](#design-direction)

## Layout

```
synapsebot/
├── sources.txt            allowlist of repository files the bot may read
├── ingest/                build step: sources -> section chunks -> src/generated/corpus.json
├── src/                   the API: request checks, BM25 search, prompt, streaming answers
├── api/                   the Vercel function that serves src/ at /api/*
├── dev/                   local server: the page and the same API on one port
├── public/                the page: HTML, CSS, vanilla JS, self-hosted fonts
├── test/                  unit tests plus the retrieval eval over the real corpus
├── eval/                  professor-style questions; run.ts is the paid answer eval
└── vercel.json            build command, function limits, security headers
```

## How an answer is made

1. `npm run index` reads the files matched by `sources.txt` and splits them into sections at their headings. The paper's HTML is split at its `h2`/`h3` ids. The build records the commit, and the function ships with the result.
2. A question is searched with BM25 over those sections. The previous question counts at half weight, so a follow-up like "why?" keeps its topic. Up to eight sections, capped at about 24,000 characters, are sent along with the orientation section (`docs/vision.md` Overview).
3. The sections go to `claude-sonnet-5-5` as `search_result` blocks with citations on, using adaptive thinking at `low` effort. Server-side refusal fallback (`fallbacks: "default"`) is enabled.
4. Claude answers in two parts, separated by a `[[practice]]` marker that the server turns into a section break. **In principle** is at most two sentences at the highest level, with no file, tool, or decision names, and is always shown. **In practice** gives the concrete mechanism and is folded until the reader opens it. Off-topic or uncovered questions get a single short reply.
5. The answer streams back to the page. Each citation becomes a numbered footnote that links to the file on GitHub at the exact commit the corpus was built from.

If nothing in the corpus matches, the bot answers with a fixed message and makes no API call.

## Running it

Everything runs from this directory with Node 24.

| Command | What it does |
|---|---|
| `npm ci` | Install dependencies |
| `npm run index` | Rebuild the corpus from `sources.txt` |
| `npm run ci` | Index, typecheck, run tests plus the retrieval eval, and check that the Vercel entry loads as plain Node ESM. No API calls |
| `npm run dev` | Local server at http://localhost:8787. Needs `.env.local` (below) |
| `npm run eval -- --yes` | Paid answer eval, about $0.02–0.03 per question; answers go to `eval/results/` |
| `npm run deploy` | `npm run ci`, then build here and publish the result to Vercel |

For local development, create `.env.local`, which is git-ignored, holding `ANTHROPIC_API_KEY=...` and `ACCESS_CODE=...`. Never commit it.

### Why Vercel, and why it builds here

The first version ran on Cloudflare Workers. It moved to Vercel because Anthropic's firewall intermittently blocks requests from Cloudflare's shared egress IPs. That failure took down 30–50% of askwillard's requests before it moved to Vercel for the same reason.

The corpus is built from files outside `synapsebot/`, and a Vercel build can't read outside its project's root directory. So `npm run deploy` builds here, with the whole repository present, and uploads the finished output (`vercel build`, then `vercel deploy --prebuilt`). Don't connect the Vercel project to the Git repository: a Git-triggered build would run without the thesis files.

### First deployment

1. Log in and link this directory to a new Vercel project: `npx vercel login`, then `npx vercel link` from `synapsebot/`. The link lives in `.vercel/`, which is git-ignored.
2. Add the secrets to the project. Each command prompts for the value, so it never lands in shell history: `npx vercel env add ANTHROPIC_API_KEY production`, then `npx vercel env add ACCESS_CODE production`. Use a dedicated Anthropic API key in its own workspace, not askwillard's, so each app can be capped and revoked on its own.
3. `npm run deploy`. Vercel prints the production URL.
4. In the Claude Console, set a monthly spend limit on that workspace. This is the hard cap on what a leaked access code can cost you.

To change the class passphrase, update `ACCESS_CODE` in the Vercel project settings (or `vercel env rm` then `add`) and redeploy. Existing visitors are asked for the new one on their next question.

## Keeping it current

The bot answers from the commit it was last deployed at, and the page's colophon names that commit. After the docs change, run `npm run deploy` again. To make a new file readable, add it to `sources.txt` on purpose. The build fails if a file contains an email address, an API key, or a private key.

When the bot misses a question it should answer, add the question to `eval/questions.json` along with the file that answers it. `npm run ci` then checks retrieval for that question on every change.

## Privacy and cost

- **Visitor data.** No accounts. Questions are sent to the Anthropic API to be answered and are not stored by SynapseBot. Anthropic retains API data under its commercial terms, so the colophon tells visitors their questions go to Anthropic. The visitor's IP is used only as the rate-limit key, held in memory for at most a minute. The one log line per answer holds counts and cited section ids, never question text.
- **The page.** No analytics or third-party requests. Fonts are self-hosted, because loading Google Fonts would send every visitor's IP to Google, which is a GDPR problem. A strict Content-Security-Policy is set in `vercel.json`.
- **What is published.** Only allowlisted files. Kept out on purpose: the GPL-3.0 NL2Bash data, other authors' papers under `research-methods/*/references`, `docs/notes`, `docs/retrospective.md`, and anything with study-participant data. Keep participant data out of `sources.txt` for good: it falls under the study's ethics approval and data-protection law.
- **Cost.** A typical question sends about 8–10k input tokens and gets a few hundred output tokens: roughly $0.02–0.03 on Sonnet 5.5. The rate limit is 8 questions per minute per visitor. It is counted per function instance, so it's a brake rather than a guarantee, and the Anthropic spend limit is the hard cap.

## Design direction

The owner's standing preference: **the page reads like a book, because the subject is academic.** Keep it clean, professional, and sophisticated, with typewriter accents. In practice:

- EB Garamond for reading text: justified, hyphenated, old-style numerals, indented follow-on paragraphs.
- Courier Prime (typewriter) for the reader's own voice and for apparatus: questions, labels, running heads, file paths, footnote numbers.
- Warm paper and a single ink, with one oxblood accent. A dark "night reading" variant follows the system setting until the reader picks one with the Light/Dark toggle; the choice is remembered in that browser only.
- Answers as "In principle" (always visible) and "In practice" (folded, opened with "read more"), labelled in the typewriter apparatus voice. These terms replace "high level / low level" on purpose.
- Book furniture instead of app chrome: a running head, a drop cap, a "Contents" list of starter questions, footnotes under each answer, a fleuron and a colophon. No chat bubbles, avatars, or cards.

Keep future UI changes within this direction.
