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
//
// Sizing and notes policy (2026-10-07 revision): body text sized for reading from
// the back of a defense room, not a laptop screen — titles 34-44pt, body 18-20pt,
// card text no smaller than 15pt. Speaker notes are short spoken cues (1-2
// sentences), not a transcript — the substance already lives on the slide itself
// (bullets, boxes, charts) and in the thesis documents; notes only add the one
// thing not visible on the slide (a quote, a transition, why this slide matters).
// Where a slide used to carry two distinct points under one long note, it was
// split into two slides instead of writing a long note to cover both.
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
          x: 0.9, y: 2.4, w: 11.5, h: 1.7,
          fontFace: THEME.headFontFace, fontSize: 44, bold: true,
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
          x: 0.9, y: 4.2, w: 11.5, h: 1.8,
          fontFace: THEME.bodyFontFace, fontSize: 22,
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
          x: 0.6, y: 0.4, w: 12.1, h: 1.0,
          fontFace: THEME.headFontFace, fontSize: 34, bold: true,
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
          x: 0.6, y: 1.6, w: 12.1, h: 5.45,
          fontFace: THEME.bodyFontFace, fontSize: 19,
          color: C.background1, align: "left", valign: "top",
        },
        text: "",
      },
    },
    { text: { text: "SynapseOS — Thesis 1 Proposal Defense", options: { x: 0.6, y: 7.15, w: 8, h: 0.3, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2, align: "left" } } },
  ],
  slideNumber: { x: 12.6, y: 7.15, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2 },
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
          x: 0.6, y: 0.4, w: 12.1, h: 1.0,
          fontFace: THEME.headFontFace, fontSize: 34, bold: true,
          color: C.accent1, align: "left", valign: "top",
        },
        text: "",
      },
    },
    { text: { text: "SynapseOS — Thesis 1 Proposal Defense", options: { x: 0.6, y: 7.15, w: 8, h: 0.3, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2, align: "left" } } },
  ],
  slideNumber: { x: 12.6, y: 7.15, fontFace: THEME.bodyFontFace, fontSize: 10, color: C.background2 },
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
      paraSpaceAfter: 16,
      fontSize: opts.fontSize || 19,
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
    fontFace: "Courier New", fontSize: 16, color: C.background2,
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
    x, y: y + 0.12, w, h: 0.55,
    fontFace: "Courier New", fontSize: 17, bold: true, color,
    align: "center", valign: "middle", isTextBox: true, margin: 0,
  });
  slide.addText(sub, {
    x: x + 0.12, y: y + 0.68, w: w - 0.24, h: h - 0.8,
    fontFace: THEME.bodyFontFace, fontSize: 13, color: C.background1,
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
    fontFace: THEME.headFontFace, fontSize: 44, bold: true, color,
    align: "center", valign: "bottom", isTextBox: true, margin: 0,
  });
  slide.addText(label, {
    x: x + 0.15, y: y + h - 0.6, w: w - 0.3, h: 0.55,
    fontFace: THEME.bodyFontFace, fontSize: 13, color: C.background2,
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
      { text: "Designing a Conversational Interface Layer for Personal Computing\n\n", options: { breakLine: true, fontSize: 22, color: C.background1 } },
      { text: "Thesis 1 Proposal Defense — Chapters 1–3\n", options: { breakLine: true, fontSize: 16, color: C.background2 } },
      { text: "Alexandra Sulit · Willard Soriano · Steven Evian Lozano\n", options: { breakLine: true, fontSize: 16, color: C.background2 } },
      { text: "Department of Computer Science, Mapúa University – Makati", options: { fontSize: 16, color: C.background2 } },
    ],
    { placeholder: "subtitle" }
  );
  s.addNotes(
    "This is a proposal defense for SynapseOS, Chapters 1 through 3 — the research plan, not final results, since the user study hasn't run yet."
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
    "Nobody has built and evaluated a conversational layer over the full Linux desktop session — that's the gap we're targeting."
  );
}

// ---------------------------------------------------------------------------
// 3. Problem statement
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Problem Statement", { placeholder: "title" });
  s.addShape(pres.ShapeType.roundRect, {
    x: 1.0, y: 2.2, w: 11.3, h: 2.8, rectRadius: 0.1,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1 },
  });
  s.addText(
    "The capability of a personal computer is unevenly reachable. This study addresses the absence of a conversational interface layer — one that accepts intent in ordinary language and carries it out through the standard system toolchain — that has been empirically evaluated against the conventional graphical workflow, specifically for whether it changes what people of differing technical fluency can accomplish.",
    {
      x: 1.4, y: 2.5, w: 10.5, h: 2.2,
      fontFace: THEME.bodyFontFace, fontSize: 19, color: C.background1,
      align: "left", valign: "middle", isTextBox: true, margin: 0, italic: true,
    }
  );
  s.addNotes("Direct from Chapter 1 — the frame for everything that follows.");
}

// ---------------------------------------------------------------------------
// 4. Research questions
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
    ], { fontSize: 18 }),
    { placeholder: "body" }
  );
  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 6.25, w: 12.1, h: 0.75, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { type: "none" },
  });
  s.addText(
    [
      { text: "RQ1 leads on purpose ", options: { bold: true, color: C.accent1 } },
      { text: "— the algorithm and the interface are two independent contributions, not one subordinate to the other.", options: { color: C.background2 } },
    ],
    { x: 0.9, y: 6.25, w: 11.5, h: 0.75, fontFace: THEME.bodyFontFace, fontSize: 15, valign: "middle", isTextBox: true, margin: 0 }
  );
  s.addNotes(
    "RQ1 moved to the front after an earlier adviser review asked whether any contribution would survive without the interface — objectives mirror these four one-to-one."
  );
}

// ---------------------------------------------------------------------------
// 5. Conceptual framework
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Conceptual Framework", { placeholder: "title" });
  s.addText("A four-layer pipeline, common to every language-mediated computing interface surveyed in Chapter 2:", {
    x: 0.6, y: 1.6, w: 12.1, h: 0.55,
    fontFace: THEME.bodyFontFace, fontSize: 17, color: C.background2, isTextBox: true, margin: 0,
  });

  const boxes = [
    { label: "INPUT", sub: "Intent arrives as text (or speech/gesture)", color: THEME.colors.accent3 },
    { label: "REASONING", sub: "Locally-hosted small language model parses intent", color: THEME.colors.accent1 },
    { label: "ACTION", sub: "Translated into shell commands via the standard toolchain", color: THEME.colors.accent2 },
    { label: "OS INTEGRATION", sub: "Spans the full graphical session, not one app", color: THEME.colors.accent4 },
  ];
  const boxW = 2.65, gap = 0.55, startX = 0.7, boxY = 2.55, boxH = 2.3;
  boxes.forEach((b, i) => {
    const x = startX + i * (boxW + gap);
    pipelineBox(s, { x, y: boxY, w: boxW, h: boxH, label: b.label, sub: b.sub, color: b.color });
    if (i < boxes.length - 1) {
      s.addText("▸", {
        x: x + boxW, y: boxY, w: gap, h: boxH,
        fontSize: 24, color: C.background2, align: "center", valign: "middle", isTextBox: true, margin: 0,
      });
    }
  });

  s.addText(
    "SynapseOS instantiates this pipeline directly — the recoverability algorithm (RQ1) is the part of the Action layer that decides what's safe to run without asking.",
    {
      x: 0.7, y: 5.25, w: 11.9, h: 1.0,
      fontFace: THEME.bodyFontFace, fontSize: 16, italic: true, color: C.background1, isTextBox: true, margin: 0,
    }
  );
  s.addNotes(
    "SynapseOS instantiates this pipeline directly, and the recoverability algorithm lives at the Action layer."
  );
}

// ---------------------------------------------------------------------------
// 6. What it looks like (placeholder capture)
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("What It Looks Like", { placeholder: "title" });
  terminalPlaceholder(s, {
    x: 1.3, y: 1.75, w: 10.7, h: 5.0,
    caption: "[ placeholder — terminal capture: a natural-language request,\nthe generated command, the confirmation gate, and the result ]",
  });
  s.addNotes(
    "Placeholder for a real recorded exchange — this already runs today, on real hardware."
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
    "A pattern list judges a command by its name, not its effect — the naive safe fix is safe but useless."
  );
}

// ---------------------------------------------------------------------------
// 8. The algorithm - what it claims
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("The Algorithm (RQ1) — What It Claims", { placeholder: "title" });

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 1.6, w: 5.85, h: 2.75, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1.2 },
  });
  s.addText("Composition through wrappers", {
    x: 0.85, y: 1.8, w: 5.35, h: 0.55, fontFace: "Courier New", fontSize: 17, bold: true, color: C.accent1, isTextBox: true, margin: 0,
  });
  s.addText(
    "Sees through find -exec, xargs, loops, command/process substitution, pipelines, redirects, sh -c, sudo and its relatives — earlier parts of a line are visible to later ones.",
    { x: 0.85, y: 2.4, w: 5.35, h: 1.85, fontFace: THEME.bodyFontFace, fontSize: 15, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 6.75, y: 1.6, w: 5.85, h: 2.75, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1.2 },
  });
  s.addText("Resolution of run-time targets", {
    x: 7.0, y: 1.8, w: 5.35, h: 0.55, fontFace: "Courier New", fontSize: 17, bold: true, color: C.accent1, isTextBox: true, margin: 0,
  });
  s.addText(
    "Where a target is computed while the command runs (rm $(ls *.log), find -delete, xargs rm), a read-only dry run learns the concrete targets — so a capture plan can be built before anything executes.",
    { x: 7.0, y: 2.4, w: 5.35, h: 1.85, fontFace: THEME.bodyFontFace, fontSize: 15, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 4.55, w: 12.0, h: 0.85, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent2, width: 1 },
  });
  s.addText(
    [
      { text: "Deliberately narrow: ", options: { bold: true, color: C.accent2 } },
      { text: "a command the analysis doesn't model is unknown — and unknown fails closed. It asks, and captures nothing. No broader claim is made.", options: { color: C.background1 } },
    ],
    { x: 0.9, y: 4.55, w: 11.4, h: 0.85, fontFace: THEME.bodyFontFace, fontSize: 15, valign: "middle", isTextBox: true, margin: 0 }
  );

  terminalPlaceholder(s, {
    x: 0.6, y: 5.55, w: 12.0, h: 1.45,
    caption: "[ placeholder — terminal capture: the safety gate catching a destructive\ncommand and asking for confirmation before it runs ]",
  });

  s.addNotes(
    "Two abilities only, deliberately narrow — anything the analysis doesn't model is unknown and fails closed."
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
      x: col.x, y: 1.7, w: 5.85, h: 4.7, rectRadius: 0.08,
      fill: { color: THEME.colors.dk2 }, line: { color: col.color, width: 1.2 },
    });
    s.addText(col.title, {
      x: col.x + 0.3, y: 1.95, w: 5.25, h: 0.55, fontFace: "Courier New", fontSize: 19, bold: true, color: col.color, isTextBox: true, margin: 0,
    });
    s.addText(
      col.lines.map((t, i) => ({ text: t, options: { bullet: { code: "25B8" }, breakLine: i !== col.lines.length - 1, paraSpaceAfter: 12, fontSize: 16, color: C.background1 } })),
      { x: col.x + 0.3, y: 2.6, w: 5.25, h: 3.7, isTextBox: true, margin: 0 }
    );
  });

  s.addNotes(
    "Two independent contributions, reported separately because they're established separately — no participants for RQ1, 40 for RQ2 through 4."
  );
}

// ---------------------------------------------------------------------------
// 10. Silent data loss - chart
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CHART" });
  s.addText("Algorithm Result — Silent Data Loss", { placeholder: "title" });

  const cats = ["Pattern list", "Fail-closed list", "Algorithm"];

  s.addChart(
    pres.ChartType.bar,
    [{ name: "Silent data loss (%)", labels: cats, values: [41.2, 0.0, 0.0] }],
    {
      x: 2.4, y: 1.7, w: 8.5, h: 4.9,
      showTitle: true, title: "Lower is better — held-out corpus, 777 commands (round 7)", titleColor: THEME.colors.lt1, titleFontSize: 15, titleFontFace: "+mn-lt",
      showValue: true, dataLabelPosition: "outEnd", dataLabelFormatCode: "0.0", dataLabelColor: THEME.colors.lt1, dataLabelFontSize: 14, dataLabelFontFace: "+mn-lt",
      chartColors: [DANGER, GOOD, GOOD],
      catAxisLabelColor: THEME.colors.lt2, catAxisLabelFontSize: 13, catAxisLabelFontFace: "+mn-lt",
      valAxisLabelColor: THEME.colors.lt2, valAxisLabelFontSize: 13, valAxisLabelFontFace: "+mn-lt",
      valAxisMaxVal: 50,
      valGridLine: { color: THEME.colors.dk2, size: 1 },
      catGridLine: { style: "none" },
      showLegend: false,
    }
  );

  s.addText(
    "Out of 405 commands that touch something outside the filesystem or lose data.",
    { x: 0.6, y: 6.75, w: 12.1, h: 0.4, fontFace: THEME.bodyFontFace, fontSize: 13, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "The pattern list silently loses data 41% of the time; the fail-closed list and our algorithm are both exactly zero."
  );
}

// ---------------------------------------------------------------------------
// 11. Restoration coverage - chart
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CHART" });
  s.addText("Algorithm Result — Restoration Coverage", { placeholder: "title" });

  const cats = ["Pattern list", "Fail-closed list", "Algorithm"];

  s.addChart(
    pres.ChartType.bar,
    [{ name: "Restoration coverage (%)", labels: cats, values: [41.5, 0.0, 89.5] }],
    {
      x: 2.4, y: 1.7, w: 8.5, h: 4.9,
      showTitle: true, title: "Higher is better — of the 325 executable losses", titleColor: THEME.colors.lt1, titleFontSize: 15, titleFontFace: "+mn-lt",
      showValue: true, dataLabelPosition: "outEnd", dataLabelFormatCode: "0.0", dataLabelColor: THEME.colors.lt1, dataLabelFontSize: 14, dataLabelFontFace: "+mn-lt",
      chartColors: [NEUTRAL, NEUTRAL, GOOD],
      catAxisLabelColor: THEME.colors.lt2, catAxisLabelFontSize: 13, catAxisLabelFontFace: "+mn-lt",
      valAxisLabelColor: THEME.colors.lt2, valAxisLabelFontSize: 13, valAxisLabelFontFace: "+mn-lt",
      valAxisMaxVal: 100,
      valGridLine: { color: THEME.colors.dk2, size: 1 },
      catGridLine: { style: "none" },
      showLegend: false,
    }
  );

  s.addText(
    "Single-annotator corpus, one held-out round — a second annotator and a larger sample are still open (open-problems.md, row 27).",
    { x: 0.6, y: 6.75, w: 12.1, h: 0.4, fontFace: THEME.bodyFontFace, fontSize: 13, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "Our algorithm restores 89.5% of executable losses; the fail-closed list restores none, by construction — it only ever asks, never captures."
  );
}

// ---------------------------------------------------------------------------
// 12. Interface evaluation plan
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
    "Within-subjects against each participant's own OS — the gap between fluent-in-neither and fluent-in-both participants is the primary outcome."
  );
}

// ---------------------------------------------------------------------------
// 13. Scope & sequencing
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Scope & Sequencing", { placeholder: "title" });

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 1.7, w: 12.1, h: 2.1, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent2, width: 1.2 },
  });
  s.addText(
    [
      { text: "Responding directly to “manage scope carefully”:\n\n", options: { bold: true, color: C.accent2, breakLine: true, fontSize: 18 } },
      { text: "the algorithm and the GUI-mode session launch are the mandatory deliverable. Model fine-tuning and the full comparative study are real and still planned, but the first things to shrink if time runs out.", options: { color: C.background1, fontSize: 17 } },
    ],
    { x: 0.95, y: 1.9, w: 11.4, h: 1.75, fontFace: THEME.bodyFontFace, isTextBox: true, margin: 0, valign: "middle" }
  );

  s.addText(
    "That reorder of priority — not of ambition — is our written answer to the adviser's own scope concern from an earlier review.",
    { x: 0.6, y: 4.1, w: 12.1, h: 0.8, fontFace: THEME.bodyFontFace, fontSize: 16, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "Our adviser's own written feedback said, verbatim, to manage scope carefully — this is our answer, in writing, not just a verbal reassurance."
  );
}

// ---------------------------------------------------------------------------
// 14. Current status
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Current Status", { placeholder: "title" });

  const rows = [
    { label: "Algorithm (RQ1)", status: "Essentially done — round 7 reported", color: GOOD },
    { label: "Build — runtime & GUI session launch", status: "In progress", color: THEME.colors.accent2 },
    { label: "Ethics / IRB", status: "Instruments drafted; application not yet submitted", color: DANGER },
  ];
  let y = 1.9;
  rows.forEach((r) => {
    s.addShape(pres.ShapeType.roundRect, {
      x: 0.6, y, w: 12.1, h: 1.3, rectRadius: 0.06,
      fill: { color: THEME.colors.dk2 }, line: { type: "none" },
    });
    s.addShape(pres.ShapeType.ellipse, { x: 0.95, y: y + 0.54, w: 0.26, h: 0.26, fill: { color: r.color }, line: { type: "none" } });
    s.addText(r.label, { x: 1.4, y: y + 0.15, w: 4.9, h: 1.0, fontFace: "Courier New", fontSize: 18, bold: true, color: C.background1, valign: "middle", isTextBox: true, margin: 0 });
    s.addText(r.status, { x: 6.4, y: y + 0.15, w: 6.1, h: 1.0, fontFace: THEME.bodyFontFace, fontSize: 17, color: C.background2, valign: "middle", isTextBox: true, margin: 0 });
    y += 1.5;
  });

  s.addNotes(
    "Ethics is the real bottleneck — instruments are drafted, but the application itself hasn't been submitted yet, so it's being prioritized now."
  );
}

// ---------------------------------------------------------------------------
// 15. Known open question
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("One Question We Want to Raise Ourselves", { placeholder: "title" });

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.9, y: 1.65, w: 11.5, h: 1.7, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent2, width: 1.3 },
  });
  s.addText(
    "“How much of this result comes from the algorithm itself, versus the hand-built rule tables underneath it?”",
    { x: 1.3, y: 1.85, w: 10.7, h: 1.3, fontFace: "Courier New", fontSize: 19, italic: true, color: C.accent2, valign: "middle", align: "center", isTextBox: true, margin: 0 }
  );

  s.addText(
    bullets([
      "This is a question our adviser already asked, in an earlier review",
      "Current answer: a prose argument — the rule table is itself a list; composition and target resolution are what's built on top of it",
      "Not yet a number — no ablation has isolated the rule table's own contribution",
      "Plan to close it: an analyzer flag that routes composition/wrapper handling to “opaque”, run as a fourth comparison column against the same corpus",
    ], { fontSize: 17 }),
    { x: 0.9, y: 3.65, w: 11.5, h: 3.2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "We're naming this ourselves rather than having it asked cold — we have a concrete plan to turn the prose answer into a measured number."
  );
}

// ---------------------------------------------------------------------------
// 16. Expected contribution
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
      x, y: 1.75, w, h: 4.4, rectRadius: 0.08,
      fill: { color: THEME.colors.dk2 }, line: { type: "none" },
    });
    s.addText(st.stat, {
      x, y: 1.95, w, h: 1.05, fontFace: THEME.headFontFace, fontSize: 32, bold: true, color: st.color, align: "center", isTextBox: true, margin: 0,
    });
    s.addText(st.label, {
      x: x + 0.3, y: 3.05, w: w - 0.6, h: 2.9, fontFace: THEME.bodyFontFace, fontSize: 16, color: C.background1, align: "center", valign: "top", isTextBox: true, margin: 0,
    });
  });

  s.addNotes(
    "Either contribution stands on its own if the other slips — that independence was a deliberate scope decision, not an accident."
  );
}

// ---------------------------------------------------------------------------
// 17. Q&A
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "TITLE" });
  s.addText("Questions", { placeholder: "title" });
  s.addText("Thank you.", { placeholder: "subtitle" });
  s.addNotes("Open floor for questions.");
}

(async () => {
  await pres.writeFile({ fileName: OUT });
  await applyTheme(OUT, THEME);
  console.log("wrote", OUT);
})();
