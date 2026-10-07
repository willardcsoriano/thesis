// Generates SynapseOS_Proposal_Defense.pptx from scratch. Rerun this after editing
// content/theme below rather than hand-editing the .pptx's XML.
//
// Setup (once): from this directory, `npm install pptxgenjs react-icons react
// react-dom sharp`, then run with `NODE_PATH="$(pwd)/node_modules" node build-deck.js`.
// node_modules is not committed — regenerate it locally when needed.
//
// The applyTheme() import below comes from the Claude Code `pptx` skill's own
// scripts/ directory (path resolved via `find ~/.claude/skills -name apply_theme.js`
// if this one has moved after a skill update) — not vendored here, since it belongs
// to that skill rather than to this repo.
const pptxgen = require("pptxgenjs");
const { applyTheme } = require("/home/willard/.claude/skills/synced/52694aae-ba96-4669-9afa-6d301ea140af_9d0ddd6e-8877-4575-a3ce-ad3e2d35de48/pptx/scripts/apply_theme.js");

const OUT = "/home/willard/projects/thesis/thesis 1/defense/SynapseOS_Proposal_Defense.pptx";

const THEME = {
  name: "SynapseOS Terminal",
  headFontFace: "Courier New",
  bodyFontFace: "Calibri",
  colors: {
    dk1: "0D1117", // near-black terminal background
    lt1: "C9D1D9", // primary light text
    dk2: "161B22", // card / panel background
    lt2: "8B949E", // muted secondary text
    accent1: "3FB950", // terminal green - primary accent
    accent2: "F0A500", // amber - caution / open-question accent
    accent3: "58A6FF", // muted blue - secondary data
    accent4: "F85149", // red - danger / unsafe
    accent5: "8B949E",
    accent6: "C9D1D9",
    hlink: "58A6FF",
    folHlink: "8B949E",
  },
};

const DANGER = "F85149";
const NEUTRAL = "8B949E";
const GOOD = "3FB950";

const pres = new pptxgen();
pres.layout = "LAYOUT_WIDE"; // 13.3 x 7.5
pres.theme = { headFontFace: THEME.headFontFace, bodyFontFace: THEME.bodyFontFace };
pres.company = "SynapseOS Thesis";

const C = pres.SchemeColor;

// ---------------------------------------------------------------------------
// Layouts
// ---------------------------------------------------------------------------

// TITLE layout
pres.defineSlideMaster({
  title: "TITLE",
  background: { color: THEME.colors.dk1 },
  objects: [
    {
      placeholder: {
        options: {
          name: "title",
          type: "title",
          x: 0.9, y: 2.5, w: 11.5, h: 1.6,
          fontFace: THEME.headFontFace, fontSize: 40, bold: true,
          color: C.accent1, align: "left", valign: "bottom",
        },
        text: "",
      },
    },
    {
      placeholder: {
        options: {
          name: "subtitle",
          type: "body",
          x: 0.9, y: 4.2, w: 11.5, h: 1.6,
          fontFace: THEME.bodyFontFace, fontSize: 18,
          color: C.background1, align: "left", valign: "top",
        },
        text: "",
      },
    },
  ],
});

// CONTENT layout - title pinned top-left, identical on every content slide
pres.defineSlideMaster({
  title: "CONTENT",
  background: { color: THEME.colors.dk1 },
  objects: [
    {
      placeholder: {
        options: {
          name: "title",
          type: "title",
          x: 0.6, y: 0.45, w: 12.1, h: 0.9,
          fontFace: THEME.headFontFace, fontSize: 30, bold: true,
          color: C.accent1, align: "left", valign: "top",
        },
        text: "",
      },
    },
    {
      placeholder: {
        options: {
          name: "body",
          type: "body",
          x: 0.6, y: 1.55, w: 12.1, h: 5.5,
          fontFace: THEME.bodyFontFace, fontSize: 16,
          color: C.background1, align: "left", valign: "top",
        },
        text: "",
      },
    },
    { text: { text: "SynapseOS — Thesis 1 Proposal Defense", options: { x: 0.6, y: 7.12, w: 8, h: 0.3, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2, align: "left" } } },
  ],
  slideNumber: { x: 12.6, y: 7.12, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2 },
});

// CHART layout - same title position as CONTENT for consistency
pres.defineSlideMaster({
  title: "CHART",
  background: { color: THEME.colors.dk1 },
  objects: [
    {
      placeholder: {
        options: {
          name: "title",
          type: "title",
          x: 0.6, y: 0.45, w: 12.1, h: 0.9,
          fontFace: THEME.headFontFace, fontSize: 30, bold: true,
          color: C.accent1, align: "left", valign: "top",
        },
        text: "",
      },
    },
    { text: { text: "SynapseOS — Thesis 1 Proposal Defense", options: { x: 0.6, y: 7.12, w: 8, h: 0.3, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2, align: "left" } } },
  ],
  slideNumber: { x: 12.6, y: 7.12, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2 },
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function bullets(items, opts = {}) {
  const arr = items.map((t, i) => ({
    text: t,
    options: {
      bullet: { code: "25B8" },
      breakLine: i !== items.length - 1,
      paraSpaceAfter: 14,
      fontSize: opts.fontSize || 16,
      color: C.background1,
    },
  }));
  return arr;
}

// A styled "terminal panel" placeholder box - stands in for a real capture
function terminalPlaceholder(slide, { x, y, w, h, caption }) {
  slide.addShape(pres.ShapeType.roundRect, {
    x, y, w, h, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 },
    line: { color: THEME.colors.lt2, width: 1, dashType: "dash" },
    shadow: { type: "outer", color: "000000", opacity: 0.4, blur: 6, offset: 3, angle: 90 },
  });
  // fake terminal chrome dots
  const dotColors = [DANGER, "F0A500", GOOD];
  dotColors.forEach((col, i) => {
    slide.addShape(pres.ShapeType.ellipse, {
      x: x + 0.22 + i * 0.26, y: y + 0.2, w: 0.14, h: 0.14,
      fill: { color: col }, line: { type: "none" },
    });
  });
  slide.addText(caption, {
    x: x + 0.4, y: y + 0.6, w: w - 0.8, h: h - 1.0,
    fontFace: "Courier New", fontSize: 14, color: C.background2,
    align: "center", valign: "middle", italic: true, isTextBox: true, margin: 0,
  });
}

function pipelineBox(slide, { x, y, w, h, label, sub, color }) {
  slide.addShape(pres.ShapeType.roundRect, {
    x, y, w, h, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 },
    line: { color, width: 1.5 },
  });
  slide.addText(label, {
    x, y: y + 0.12, w, h: 0.5,
    fontFace: "Courier New", fontSize: 15, bold: true, color,
    align: "center", valign: "middle", isTextBox: true, margin: 0,
  });
  slide.addText(sub, {
    x: x + 0.12, y: y + 0.62, w: w - 0.24, h: h - 0.74,
    fontFace: THEME.bodyFontFace, fontSize: 11, color: C.background1,
    align: "center", valign: "top", isTextBox: true, margin: 0,
  });
}

function statCallout(slide, { x, y, w, h, stat, label, color }) {
  slide.addShape(pres.ShapeType.roundRect, {
    x, y, w, h, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 },
    line: { type: "none" },
  });
  slide.addText(stat, {
    x, y: y + 0.15, w, h: h - 0.75,
    fontFace: THEME.headFontFace, fontSize: 40, bold: true, color,
    align: "center", valign: "bottom", isTextBox: true, margin: 0,
  });
  slide.addText(label, {
    x: x + 0.15, y: y + h - 0.6, w: w - 0.3, h: 0.55,
    fontFace: THEME.bodyFontFace, fontSize: 11, color: C.background2,
    align: "center", valign: "top", isTextBox: true, margin: 0,
  });
}

// ---------------------------------------------------------------------------
// 1. Title
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "TITLE" });
  s.addText("SynapseOS", { placeholder: "title" });
  s.addText(
    [
      { text: "Designing a Conversational Interface Layer for Personal Computing\n\n", options: { breakLine: true, fontSize: 20, color: C.background1 } },
      { text: "Thesis 1 Proposal Defense — Chapters 1–3\n", options: { breakLine: true, fontSize: 14, color: C.background2 } },
      { text: "Alexandra Sulit · Willard Soriano · Steven Evian Lozano\n", options: { breakLine: true, fontSize: 14, color: C.background2 } },
      { text: "Department of Computer Science, Mapúa University – Makati", options: { fontSize: 14, color: C.background2 } },
    ],
    { placeholder: "subtitle" }
  );
  s.addNotes(
    "Good [morning/afternoon]. We're presenting the Thesis 1 proposal for SynapseOS — a conversational interface layer for personal computing. " +
    "This is a proposal defense: we're defending the research plan and methodology in Chapters 1 through 3, not final results — the comparative user study hasn't run yet. " +
    "Walk through: the gap we're addressing, the two research contributions, how each is evaluated, where we stand against the timeline, and one open question we want to be upfront about."
  );
}

// ---------------------------------------------------------------------------
// 2. The gap
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("The Gap", { placeholder: "title" });
  s.addText(
    bullets([
      "Nearly any operation a computer supports can be expressed on the command line — but only by someone who can write it",
      "Everyone else is limited to what a graphical desktop's designers anticipated",
      "LLMs have reopened a decades-old research thread (Berkeley UNIX Consultant, 1988) with far greater capability",
      "But no implemented, evaluated system places a conversational layer over the whole Linux desktop session — existing work covers only the shell, or only individual applications",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "The command line can express almost anything a computer supports, but it demands memorized syntax. The GUI made the translation visual instead of textual, but didn't remove it — you still adapt to the machine. " +
    "Large language models reopened this thread — the idea itself goes back to the Berkeley UNIX Consultant in the late 1980s — but recent work either stays at the shell level, or automates individual applications, or treats Linux as one of several evaluation platforms rather than a real deployment target. " +
    "Nobody has built and evaluated a conversational layer over the full Linux desktop session. That's the gap."
  );
}

// ---------------------------------------------------------------------------
// 3. Problem statement
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Problem Statement", { placeholder: "title" });
  s.addShape(pres.ShapeType.roundRect, {
    x: 1.2, y: 2.3, w: 10.9, h: 2.6, rectRadius: 0.1,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1 },
  });
  s.addText(
    "The capability of a personal computer is unevenly reachable. This study addresses the absence of a conversational interface layer — one that accepts intent in ordinary language and carries it out through the standard system toolchain — that has been empirically evaluated against the conventional graphical workflow, specifically for whether it changes what people of differing technical fluency can accomplish.",
    {
      x: 1.6, y: 2.6, w: 10.1, h: 2.0,
      fontFace: THEME.bodyFontFace, fontSize: 16, color: C.background1,
      align: "left", valign: "middle", isTextBox: true, margin: 0, italic: true,
    }
  );
  s.addNotes(
    "Direct from Chapter 1. The core problem: computer capability is unevenly reachable depending on which interface you're fluent in. " +
    "We're addressing the absence of a conversational layer that's actually been empirically evaluated against the graphical workflow it would supplement — and evaluated specifically on whether it narrows the fluency gap, not just whether it works at all."
  );
}

// ---------------------------------------------------------------------------
// 4. Research questions & objectives
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Research Questions", { placeholder: "title" });
  s.addText(
    bullets([
      "RQ1 — Can we find what a generated command will do before it runs, and capture enough to undo it — losing less data and asking less often than a pattern list?",
      "RQ2 — How do we let someone direct a whole Linux desktop in ordinary language?",
      "RQ3 — What confirmation and recovery design lets a user undo a mistake, including one they approved themselves?",
      "RQ4 — Does this narrow the gap for people fluent in neither the command line nor the GUI?",
    ]),
    { placeholder: "body" }
  );
  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 6.3, w: 12.1, h: 0.65, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { type: "none" },
  });
  s.addText(
    [
      { text: "RQ1 leads on purpose ", options: { bold: true, color: C.accent1 } },
      { text: "— the algorithm and the interface are two independent contributions, not one subordinate to the other.", options: { color: C.background2 } },
    ],
    { x: 0.9, y: 6.3, w: 11.5, h: 0.65, fontFace: THEME.bodyFontFace, fontSize: 13, valign: "middle", isTextBox: true, margin: 0 }
  );
  s.addNotes(
    "Four research questions, reordered deliberately — RQ1 is the algorithm question, ahead of the three interface questions. " +
    "That reorder happened after our adviser asked a pointed question in an earlier review: if you removed the conversational interface entirely, would a research contribution still remain? " +
    "For an earlier draft the honest answer was no. So we restructured: the recoverability algorithm is now RQ1, evaluated independently against a labeled corpus, no participants needed. RQ2 through 4 are the interface questions, which the 40-participant user study answers. " +
    "Full wording: RQ1 asks whether we can find a generated command's effects before it runs — seeing through wrappers and resolving run-time targets — losing less data silently than a pattern list, and asking less often than a list that refuses everything unfamiliar. " +
    "RQ2 is the design question for the interface itself. RQ3 is about confirmation and undo. RQ4 is the comparative question — does this narrow the gap between technically fluent and non-fluent users. " +
    "Our four objectives mirror these one-to-one: develop and evaluate the algorithm against two baselines; design the interface layer; build the parsing/execution pipeline with the confirmation gate and undo; and run the controlled comparison."
  );
}

// ---------------------------------------------------------------------------
// 5. Conceptual framework
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Conceptual Framework", { placeholder: "title" });
  s.addText("A four-layer pipeline, common to every language-mediated computing interface surveyed in Chapter 2:", {
    x: 0.6, y: 1.55, w: 12.1, h: 0.5,
    fontFace: THEME.bodyFontFace, fontSize: 15, color: C.background2, isTextBox: true, margin: 0,
  });

  const boxes = [
    { label: "INPUT", sub: "Intent arrives as text (or speech/gesture)", color: THEME.colors.accent3 },
    { label: "REASONING", sub: "Locally-hosted small language model parses intent", color: THEME.colors.accent1 },
    { label: "ACTION", sub: "Translated into shell commands via the standard toolchain", color: THEME.colors.accent2 },
    { label: "OS INTEGRATION", sub: "Spans the full graphical session, not one app", color: THEME.colors.accent4 },
  ];
  const boxW = 2.65, gap = 0.55, startX = 0.7, boxY = 2.6, boxH = 2.1;
  boxes.forEach((b, i) => {
    const x = startX + i * (boxW + gap);
    pipelineBox(s, { x, y: boxY, w: boxW, h: boxH, label: b.label, sub: b.sub, color: b.color });
    if (i < boxes.length - 1) {
      s.addText("▸", {
        x: x + boxW, y: boxY, w: gap, h: boxH,
        fontSize: 22, color: C.background2, align: "center", valign: "middle", isTextBox: true, margin: 0,
      });
    }
  });

  s.addText(
    "SynapseOS instantiates this pipeline directly — and the recoverability algorithm (RQ1) is the part of the Action layer that decides what's safe to run without asking.",
    {
      x: 0.7, y: 5.1, w: 11.9, h: 0.9,
      fontFace: THEME.bodyFontFace, fontSize: 14, italic: true, color: C.background1, isTextBox: true, margin: 0,
    }
  );
  s.addNotes(
    "This four-layer pipeline — Input, Reasoning, Action, OS Integration — is the organizing framework from Chapter 2's literature review, and it's modality-neutral and diagnostic: it let us place every surveyed system's main limitation at a specific layer. " +
    "SynapseOS instantiates it directly: natural language at Input, a locally-hosted small model at Reasoning, generated shell commands at Action, and the full graphical session — not a single app — at OS Integration. " +
    "The recoverability algorithm lives inside the Action layer: before a generated command runs, it decides whether it's safe to just execute, or whether to stop and ask."
  );
}

// ---------------------------------------------------------------------------
// 6. What it looks like (placeholder capture)
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("What It Looks Like", { placeholder: "title" });
  terminalPlaceholder(s, {
    x: 1.3, y: 1.7, w: 10.7, h: 5.0,
    caption: "[ placeholder — terminal capture: a natural-language request,\nthe generated command, the confirmation gate, and the result ]",
  });
  s.addNotes(
    "This is a placeholder — we'll drop in a real recorded terminal session here once we've verified it end to end. " +
    "What it will show: a plain-English request typed in, SynapseOS proposing the shell command it would run, the confirmation gate asking before anything irreversible happens, and the result. " +
    "The point of this slide is simple — this already runs, today, on real hardware."
  );
}

// ---------------------------------------------------------------------------
// 7. The algorithm - the problem
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("The Algorithm (RQ1) — The Problem", { placeholder: "title" });
  s.addText(
    bullets([
      "A generated command can destroy data before its author ever reads it",
      "Existing safeguards (an enumerated pattern list) judge a command by its name — not by what it actually does",
      "A list either lets unfamiliar commands run (unsafe), or refuses everything it doesn't recognize (safe, but protects nothing and asks constantly)",
      "We need a third option: understand the command well enough to know what it will change, before it runs",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "Letting a language model act on a real filesystem raises a problem the interface alone can't solve: a generated command can destroy data before anyone reads it. " +
    "Existing safety approaches — pattern-matching against a list of known-dangerous commands — judge a command by its name, not its effect. That means a disguised or unfamiliar form of a dangerous operation slips through. " +
    "The naive fix, refusing anything unrecognized, is safe but useless — it protects nothing and interrupts the user constantly. We wanted a third option: actually understand what a command will do before it runs."
  );
}

// ---------------------------------------------------------------------------
// 8. The algorithm - what it claims
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("The Algorithm (RQ1) — What It Claims", { placeholder: "title" });

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 1.65, w: 5.85, h: 2.5, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1.2 },
  });
  s.addText("Composition through wrappers", {
    x: 0.85, y: 1.85, w: 5.35, h: 0.5, fontFace: "Courier New", fontSize: 15, bold: true, color: C.accent1, isTextBox: true, margin: 0,
  });
  s.addText(
    "Sees through find -exec, xargs, loops, command/process substitution, pipelines, redirects, sh -c, sudo and its relatives — earlier parts of a line are visible to later ones.",
    { x: 0.85, y: 2.4, w: 5.35, h: 1.6, fontFace: THEME.bodyFontFace, fontSize: 13, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 6.75, y: 1.65, w: 5.85, h: 2.5, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1.2 },
  });
  s.addText("Resolution of run-time targets", {
    x: 7.0, y: 1.85, w: 5.35, h: 0.5, fontFace: "Courier New", fontSize: 15, bold: true, color: C.accent1, isTextBox: true, margin: 0,
  });
  s.addText(
    "Where a target is computed while the command runs (rm $(ls *.log), find -delete, xargs rm), a read-only dry run learns the concrete targets — so a capture plan can be built before anything executes.",
    { x: 7.0, y: 2.4, w: 5.35, h: 1.6, fontFace: THEME.bodyFontFace, fontSize: 13, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 4.35, w: 12.0, h: 0.75, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent2, width: 1 },
  });
  s.addText(
    [
      { text: "Deliberately narrow: ", options: { bold: true, color: C.accent2 } },
      { text: "a command the analysis doesn't model is unknown — and unknown fails closed. It asks, and captures nothing. No broader claim is made.", options: { color: C.background1 } },
    ],
    { x: 0.9, y: 4.35, w: 11.4, h: 0.75, fontFace: THEME.bodyFontFace, fontSize: 13, valign: "middle", isTextBox: true, margin: 0 }
  );

  terminalPlaceholder(s, {
    x: 0.6, y: 5.3, w: 12.0, h: 1.75,
    caption: "[ placeholder — terminal capture: the safety gate catching a destructive\ncommand and asking for confirmation before it runs ]",
  });

  s.addNotes(
    "We deliberately narrowed this claim after piloting a broader version. The algorithm claims exactly two abilities: composition through wrappers — seeing through find -exec, xargs, loops, substitution, pipelines, sudo, with earlier parts of a line visible to later ones — and resolution of run-time-computed targets, using a read-only dry run to learn concrete targets before anything executes. " +
    "Single named commands with explicit targets stay with the existing pattern list, which already handled them fine in our pilot. Anything the analysis doesn't model is unknown, and unknown fails closed — it asks, and captures nothing. We are not claiming to have solved recoverability in general; we're claiming these two specific abilities, measured against baselines. " +
    "The bottom panel is a placeholder for a real capture of the confirmation gate actually stopping a destructive command."
  );
}

// ---------------------------------------------------------------------------
// 9. Evaluation design - two independent studies
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Evaluation Design — Two Independent Studies", { placeholder: "title" });

  const cols = [
    {
      x: 0.6, title: "Algorithm study (RQ1)", color: THEME.colors.accent1,
      lines: ["Corpus-based — no participants", "Held-out set of generated commands, ground truth from sandboxed execution", "Three baselines compared: pattern list, fail-closed list, the algorithm", "Answers RQ1 on its own"],
    },
    {
      x: 6.75, title: "User study (RQ2–4)", color: THEME.colors.accent3,
      lines: ["40 participants, within-subjects", "Each person vs. their own daily OS (expert baseline)", "Task suite: OSWorld + custom cross-platform tasks", "Waits on IRB / ethics approval"],
    },
  ];
  cols.forEach((col) => {
    s.addShape(pres.ShapeType.roundRect, {
      x: col.x, y: 1.7, w: 5.85, h: 4.6, rectRadius: 0.08,
      fill: { color: THEME.colors.dk2 }, line: { color: col.color, width: 1.2 },
    });
    s.addText(col.title, {
      x: col.x + 0.3, y: 1.95, w: 5.25, h: 0.5, fontFace: "Courier New", fontSize: 17, bold: true, color: col.color, isTextBox: true, margin: 0,
    });
    s.addText(
      col.lines.map((t, i) => ({ text: t, options: { bullet: { code: "25B8" }, breakLine: i !== col.lines.length - 1, paraSpaceAfter: 10, fontSize: 14, color: C.background1 } })),
      { x: col.x + 0.3, y: 2.55, w: 5.25, h: 3.6, isTextBox: true, margin: 0 }
    );
  });

  s.addNotes(
    "We report these as two independent contributions, not one subordinate to the other, because they're established completely separately. " +
    "The algorithm study needs no participants — it's evaluated against a held-out corpus with ground truth from sandboxed execution, not human labels, against three baselines: the current pattern list, that same list flipped to fail-closed, and our algorithm. " +
    "The user study is the classic within-subjects design — each participant against their own daily operating system, using OSWorld plus a custom cross-platform task suite, measuring completion time, errors, SUS, and NASA-TLX. That study is the one still waiting on IRB approval."
  );
}

// ---------------------------------------------------------------------------
// 10. Preliminary evidence - chart
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CHART" });
  s.addText("Algorithm Result — Held-Out Corpus (777 commands, round 7)", { placeholder: "title" });

  const cats = ["Pattern list", "Fail-closed list", "Algorithm"];

  s.addChart(
    pres.ChartType.bar,
    [{ name: "Silent data loss (%)", labels: cats, values: [41.2, 0.0, 0.0] }],
    {
      x: 0.6, y: 1.6, w: 5.9, h: 4.6,
      showTitle: true, title: "Silent data loss — lower is better", titleColor: THEME.colors.lt1, titleFontSize: 13, titleFontFace: "+mn-lt",
      showValue: true, dataLabelPosition: "outEnd", dataLabelFormatCode: "0.0", dataLabelColor: THEME.colors.lt1, dataLabelFontSize: 11, dataLabelFontFace: "+mn-lt",
      chartColors: [DANGER, GOOD, GOOD],
      catAxisLabelColor: THEME.colors.lt2, catAxisLabelFontSize: 11, catAxisLabelFontFace: "+mn-lt",
      valAxisLabelColor: THEME.colors.lt2, valAxisLabelFontSize: 11, valAxisLabelFontFace: "+mn-lt",
      valAxisMaxVal: 50,
      valGridLine: { color: THEME.colors.dk2, size: 1 },
      catGridLine: { style: "none" },
      showLegend: false,
    }
  );

  s.addChart(
    pres.ChartType.bar,
    [{ name: "Restoration coverage (%)", labels: cats, values: [41.5, 0.0, 89.5] }],
    {
      x: 6.8, y: 1.6, w: 5.9, h: 4.6,
      showTitle: true, title: "Restoration coverage of executable losses — higher is better", titleColor: THEME.colors.lt1, titleFontSize: 13, titleFontFace: "+mn-lt",
      showValue: true, dataLabelPosition: "outEnd", dataLabelFormatCode: "0.0", dataLabelColor: THEME.colors.lt1, dataLabelFontSize: 11, dataLabelFontFace: "+mn-lt",
      chartColors: [NEUTRAL, NEUTRAL, GOOD],
      catAxisLabelColor: THEME.colors.lt2, catAxisLabelFontSize: 11, catAxisLabelFontFace: "+mn-lt",
      valAxisLabelColor: THEME.colors.lt2, valAxisLabelFontSize: 11, valAxisLabelFontFace: "+mn-lt",
      valAxisMaxVal: 100,
      valGridLine: { color: THEME.colors.dk2, size: 1 },
      catGridLine: { style: "none" },
      showLegend: false,
    }
  );

  s.addText(
    "325 executable losses out of 777 commands; one held-out round, single-annotator corpus — a second annotator and a larger sample are still open (open-problems.md, row 27).",
    { x: 0.6, y: 6.5, w: 12.1, h: 0.5, fontFace: THEME.bodyFontFace, fontSize: 11, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "These numbers come from the held-out, frozen-analyser round — round 7, 777 commands, ground truth from sandboxed execution, not an early pilot we're still tuning against. " +
    "Left chart: how often each approach silently loses data the user never asked to lose, out of 405 commands that touch something outside the filesystem or lose data. The pattern list does this 41.2% of the time. The fail-closed list and our algorithm are both exactly zero — indistinguishable on this metric, which is itself a finding: the verdict alone isn't the contribution. " +
    "Right chart is capture coverage — of the 325 commands that actually lose something, how many does each approach restore exactly, verified by executing in a sandbox and diffing the tree. The fail-closed list restores none, by construction — it only ever asks, never captures. The pattern list restores 41.5% through its existing undo path, though those captures aren't independently verified the way ours are. Our algorithm restores 89.5%. " +
    "So the story the two charts tell together: the pattern list is unsafe; the fail-closed list is safe but protects nothing; the algorithm is the only one that's both safe and actually useful. Across every executed, non-refused command — 631 of them — the undo restored 630 exactly, 99.8%, if asked for an even broader number. " +
    "We're upfront that this is one held-out round with a single annotator — a second annotator and a larger sample are a known next step, not a hidden gap."
  );
}

// ---------------------------------------------------------------------------
// 11. Interface evaluation plan
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Interface Evaluation Plan (RQ2–4)", { placeholder: "title" });
  s.addText(
    bullets([
      "Within-subjects: each of 40 participants against their own primary OS (Windows, macOS, or Linux)",
      "Two populations: fluent in neither interface, and fluent in both — the gap between them is the primary outcome",
      "Tasks: OSWorld benchmark + a custom cross-platform suite (file search, system monitoring, package management, text/data processing)",
      "Measures: completion time, error rate, SUS, NASA-TLX, plus a semi-structured interview",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "The comparative study is within-subjects — every participant is their own control, compared against whatever OS they already use daily. " +
    "We deliberately recruit two populations: people fluent in neither the command line nor the GUI, and people fluent in both. The gap between those two groups is the primary outcome we care about, not the raw numbers for either group alone — that's the direct test of our central claim. " +
    "Tasks come from two sources: the OSWorld benchmark, and a custom cross-platform task suite we built covering file search, process monitoring, package management, and text processing. " +
    "We measure completion time and error rate objectively, SUS and NASA-TLX for perceived usability and workload, and close with a semi-structured interview for qualitative trust and comparison data."
  );
}

// ---------------------------------------------------------------------------
// 12. Scope, sequencing & current status
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Scope, Sequencing & Current Status", { placeholder: "title" });

  s.addText(
    [
      { text: "Responding directly to “manage scope carefully”: ", options: { bold: true, color: C.accent2, breakLine: true } },
      { text: "the algorithm and the GUI-mode session launch are the mandatory deliverable. Model fine-tuning and the full comparative study are real and still planned, but the first things to shrink if time runs out.", options: { color: C.background1 } },
    ],
    { x: 0.6, y: 1.6, w: 12.1, h: 1.1, fontFace: THEME.bodyFontFace, fontSize: 15, isTextBox: true, margin: 0 }
  );

  const rows = [
    { label: "Algorithm (RQ1)", status: "Essentially done — round 7 reported", color: GOOD },
    { label: "Build — runtime & GUI session launch", status: "In progress", color: THEME.colors.accent2 },
    { label: "Ethics / IRB", status: "Instruments drafted; application not yet submitted", color: DANGER },
  ];
  let y = 3.0;
  rows.forEach((r) => {
    s.addShape(pres.ShapeType.roundRect, {
      x: 0.6, y, w: 12.1, h: 1.0, rectRadius: 0.06,
      fill: { color: THEME.colors.dk2 }, line: { type: "none" },
    });
    s.addShape(pres.ShapeType.ellipse, { x: 0.9, y: y + 0.38, w: 0.22, h: 0.22, fill: { color: r.color }, line: { type: "none" } });
    s.addText(r.label, { x: 1.3, y: y + 0.12, w: 4.6, h: 0.76, fontFace: "Courier New", fontSize: 15, bold: true, color: C.background1, valign: "middle", isTextBox: true, margin: 0 });
    s.addText(r.status, { x: 6.0, y: y + 0.12, w: 6.4, h: 0.76, fontFace: THEME.bodyFontFace, fontSize: 14, color: C.background2, valign: "middle", isTextBox: true, margin: 0 });
    y += 1.2;
  });

  s.addNotes(
    "Our adviser's own written feedback on an earlier round said, verbatim, to manage scope carefully — two studies, algorithm development, corpus creation, and interface work is a lot for one BSCS project. " +
    "Our answer, in writing: the algorithm and the GUI-mode session launch — the distro actually running SynapseOS fullscreen over a real desktop — are the mandatory deliverable. Model fine-tuning details and the full 40-participant comparative study are real and still planned, but they're the first things we'd scale back if time runs short. " +
    "Where we actually stand right now: the algorithm is essentially done, round 7 is the reported result. The build track — runtime and the GUI session launch — is in progress. Ethics is the real bottleneck: our study instruments are drafted, but the IRB application itself hasn't been submitted yet, and that's the least controllable variable in this whole plan, so it's being prioritized now rather than sequenced after everything else."
  );
}

// ---------------------------------------------------------------------------
// 13. Known open question
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("One Question We Want to Raise Ourselves", { placeholder: "title" });

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.9, y: 1.7, w: 11.5, h: 1.6, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent2, width: 1.3 },
  });
  s.addText(
    "“How much of this result comes from the algorithm itself, versus the hand-built rule tables underneath it?”",
    { x: 1.3, y: 1.9, w: 10.7, h: 1.2, fontFace: "Courier New", fontSize: 18, italic: true, color: C.accent2, valign: "middle", align: "center", isTextBox: true, margin: 0 }
  );

  s.addText(
    bullets([
      "This is a question our adviser already asked, in an earlier review",
      "Current answer: a prose argument — the rule table is itself a list; composition and target resolution are what's built on top of it",
      "Not yet a number — no ablation has isolated the rule table's own contribution",
      "Plan to close it: an analyzer flag that routes composition/wrapper handling to “opaque”, run as a fourth comparison column against the same corpus",
    ], { fontSize: 15 }),
    { x: 0.9, y: 3.6, w: 11.5, h: 3.2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "We'd rather name this ourselves than have it asked cold. Our adviser raised this exact question in an earlier review: how much of the algorithm's advantage comes from the algorithm's own logic, versus the hand-built per-command rule table it still relies on underneath. " +
    "Our current answer is a prose one, not a number: the rule table is itself a kind of list, and what we're actually claiming credit for is what's built on top of it — seeing through wrappers, and resolving run-time targets. But we haven't isolated that as a measured number yet. " +
    "The plan to close it: add a flag to the analyzer that routes wrapper and composition handling to an opaque fallback instead of resolving through it, run that as a fourth column in the same held-out comparison, and report the delta. That's a scoped, concrete next step, not an open-ended unknown."
  );
}

// ---------------------------------------------------------------------------
// 14. Expected contribution
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Expected Contribution", { placeholder: "title" });

  const stats = [
    { stat: "RQ1", label: "A composition- and resolution-aware recoverability algorithm, evaluated against two baselines on a held-out corpus", color: GOOD },
    { stat: "RQ2-3", label: "A conversational interface layer with a reversibility-based confirmation gate and undo, over an unchanged Linux desktop", color: THEME.colors.accent3 },
    { stat: "RQ4", label: "The first controlled evidence for whether conversation narrows the fluency gap between CLI and GUI users", color: THEME.colors.accent2 },
  ];
  const w = 3.9, gap = 0.2, startX = 0.6;
  stats.forEach((st, i) => {
    const x = startX + i * (w + gap);
    s.addShape(pres.ShapeType.roundRect, {
      x, y: 1.8, w, h: 4.3, rectRadius: 0.08,
      fill: { color: THEME.colors.dk2 }, line: { type: "none" },
    });
    s.addText(st.stat, {
      x, y: 2.0, w, h: 1.0, fontFace: THEME.headFontFace, fontSize: 30, bold: true, color: st.color, align: "center", isTextBox: true, margin: 0,
    });
    s.addText(st.label, {
      x: x + 0.25, y: 3.0, w: w - 0.5, h: 2.9, fontFace: THEME.bodyFontFace, fontSize: 13, color: C.background1, align: "center", valign: "top", isTextBox: true, margin: 0,
    });
  });

  s.addNotes(
    "If both evaluations land as designed, SynapseOS contributes two independent things: a recoverability algorithm that's measurably safer and more useful than the pattern-matching it extends, evaluated with no participants needed — and, pending IRB, the first controlled evidence on whether a conversational interface actually narrows the gap between people fluent in the command line and people fluent in neither existing interface. " +
    "Either one stands on its own if the other slips. That independence was itself a deliberate scope decision, not an accident."
  );
}

// ---------------------------------------------------------------------------
// 15. Q&A
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "TITLE" });
  s.addText("Questions", { placeholder: "title" });
  s.addText("Thank you.", { placeholder: "subtitle" });
  s.addNotes("Open floor for questions. Thank the panel and adviser.");
}

(async () => {
  await pres.writeFile({ fileName: OUT });
  await applyTheme(OUT, THEME);
  console.log("wrote", OUT);
})();
