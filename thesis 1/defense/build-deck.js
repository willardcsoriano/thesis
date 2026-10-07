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
// Sizing and notes policy (2026-10-07, revised a third time same day): body text
// sized for reading from the back of a defense room — titles 34-44pt, body
// 18-20pt, card text no smaller than 15pt. On-slide text is kept to short
// phrases and fragments, not full sentences — nobody reads prose off a slide.
// Speaker notes are 2-3 short, plain-language sentences per slide: enough to
// orient a listener without boring them, written as if the audience is smart
// but not a specialist in this subfield — plain words over jargon ("a safe
// test run" over "a read-only dry run", "a lookup table" over "an enumerated
// rule table"), technical terms introduced only where the slide itself can't
// avoid them (RQ wording, baseline names). Not a one-line cue, not a paragraph
// transcript. Where a slide's content genuinely split into two separate points
// too large for one short note, it was split into two slides instead.
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
    "Good [morning/afternoon]. We're presenting our Thesis 1 proposal for SynapseOS — software that lets someone control a computer by typing plain English instead of memorized commands. " +
    "This is a proposal defense, so we're presenting our plan, not final results — the comparison study with real users hasn't run yet."
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
      "Command line — full capability, syntax barrier",
      "Graphical desktop — limited to what designers anticipated",
      "LLMs reopen the thread (Berkeley UNIX Consultant, 1988) with far greater capability",
      "Missing: an implemented, evaluated system for the whole Linux desktop session",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "The command line can do almost anything, but only if you know the right words to type. A graphical desktop is easier, but limits you to the buttons someone else thought to add. " +
    "Nobody has yet built and tested a system that lets you control a whole Linux desktop just by talking to it normally — that's the gap we're filling."
  );
}

// ---------------------------------------------------------------------------
// 3. Problem statement
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Problem Statement", { placeholder: "title" });
  s.addShape(pres.ShapeType.roundRect, {
    x: 1.3, y: 2.6, w: 10.7, h: 2.0, rectRadius: 0.1,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1 },
  });
  s.addText(
    "Computer capability is unevenly reachable — gated by which interface a person is fluent in.",
    {
      x: 1.7, y: 2.6, w: 9.9, h: 2.0,
      fontFace: THEME.bodyFontFace, fontSize: 24, color: C.background1,
      align: "left", valign: "middle", isTextBox: true, margin: 0, italic: true,
    }
  );
  s.addNotes(
    "In one line: what a computer can actually do for you depends on which interface you already know how to use. " +
    "Our goal is a third option, ordinary language, that closes that gap for people regardless of technical background."
  );
}

// ---------------------------------------------------------------------------
// 4. Research questions
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Research Questions", { placeholder: "title" });
  s.addText(
    bullets([
      "RQ1 — Predict a command's effects before it runs; capture enough to undo it; beat a pattern list",
      "RQ2 — Direct a full Linux desktop in ordinary language",
      "RQ3 — Confirm and recover from mistakes, including ones the user approved",
      "RQ4 — Narrow the fluency gap between CLI and GUI non-experts",
    ], { fontSize: 19 }),
    { placeholder: "body" }
  );
  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 6.25, w: 12.1, h: 0.75, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { type: "none" },
  });
  s.addText(
    [
      { text: "RQ1 leads on purpose ", options: { bold: true, color: C.accent1 } },
      { text: "— two independent contributions, not one subordinate to the other.", options: { color: C.background2 } },
    ],
    { x: 0.9, y: 6.25, w: 11.5, h: 0.75, fontFace: THEME.bodyFontFace, fontSize: 15, valign: "middle", isTextBox: true, margin: 0 }
  );
  s.addNotes(
    "We reordered these after an earlier review — our adviser asked whether anything would be left if we removed the chat interface entirely. " +
    "So we made the safety algorithm its own standalone contribution, listed first; the other three questions are about the interface itself, and our user study answers those."
  );
}

// ---------------------------------------------------------------------------
// 5. Conceptual framework
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Conceptual Framework", { placeholder: "title" });
  s.addText("Four-layer pipeline, from Chapter 2's literature review:", {
    x: 0.6, y: 1.6, w: 12.1, h: 0.55,
    fontFace: THEME.bodyFontFace, fontSize: 18, color: C.background2, isTextBox: true, margin: 0,
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
    "SynapseOS instantiates this directly — the algorithm (RQ1) governs the Action layer.",
    {
      x: 0.7, y: 5.25, w: 11.9, h: 1.0,
      fontFace: THEME.bodyFontFace, fontSize: 18, italic: true, color: C.background1, isTextBox: true, margin: 0,
    }
  );
  s.addNotes(
    "This four-stage flow — understand the request, decide what to do, act on the computer, fit into the existing desktop — comes from our literature review in Chapter 2. " +
    "SynapseOS follows this same flow, and the safety algorithm we'll discuss next lives in the 'acting' stage, right before anything actually runs."
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
    "This slide will show a real recording once we finish capturing and checking it — someone typing a plain request, the command SynapseOS generates, a safety check, and the result. " +
    "We're showing this early to make one thing clear: this already works today, it isn't just a plan on paper."
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
      "Generated commands can destroy data silently",
      "Pattern lists judge a command by its name, not its effect",
      "Binary choice today: unsafe (let it through) or useless (refuse everything)",
      "Needed: know the command's actual effect before it runs",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "If an AI types commands for you, it might accidentally delete something important before anyone notices. Today's safety check just looks at whether a command looks familiar, not at what it actually does. " +
    "That leaves two bad options: let unfamiliar commands through and risk damage, or block everything unfamiliar and interrupt the user constantly. We want a third option."
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
    "find -exec, xargs, loops, substitution, pipelines, sudo — earlier segments visible to later ones",
    { x: 0.85, y: 2.4, w: 5.35, h: 1.85, fontFace: THEME.bodyFontFace, fontSize: 16, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 6.75, y: 1.6, w: 5.85, h: 2.75, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1.2 },
  });
  s.addText("Resolution of run-time targets", {
    x: 7.0, y: 1.8, w: 5.35, h: 0.55, fontFace: "Courier New", fontSize: 17, bold: true, color: C.accent1, isTextBox: true, margin: 0,
  });
  s.addText(
    "Targets computed at run time (rm $(ls *.log), find -delete, xargs rm) resolved via a read-only dry run before execution",
    { x: 7.0, y: 2.4, w: 5.35, h: 1.85, fontFace: THEME.bodyFontFace, fontSize: 16, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 4.55, w: 12.0, h: 0.85, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent2, width: 1 },
  });
  s.addText(
    [
      { text: "Deliberately narrow: ", options: { bold: true, color: C.accent2 } },
      { text: "unmodeled commands are unknown → fail closed, ask, capture nothing.", options: { color: C.background1 } },
    ],
    { x: 0.9, y: 4.55, w: 11.4, h: 0.85, fontFace: THEME.bodyFontFace, fontSize: 16, valign: "middle", isTextBox: true, margin: 0 }
  );

  terminalPlaceholder(s, {
    x: 0.6, y: 5.55, w: 12.0, h: 1.45,
    caption: "[ placeholder — terminal capture: the safety gate catching a destructive\ncommand and asking for confirmation before it runs ]",
  });

  s.addNotes(
    "Our algorithm does two things, deliberately, and nothing more. First, it can see through common tricks for hiding what a command really does — loops, shortcuts, chaining several commands together. Second, when it can't know a command's target until the moment it runs, it does a safe test run first to find out exactly what would be affected. " +
    "If a command is too unusual for it to understand, it simply stops and asks — it never guesses."
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
      lines: ["Corpus-based — no participants", "Held-out commands, sandboxed ground truth", "Three baselines: pattern list, fail-closed list, algorithm", "Answers RQ1 independently"],
    },
    {
      x: 6.75, title: "User study (RQ2–4)", color: THEME.colors.accent3,
      lines: ["40 participants, within-subjects", "Each person vs. their own daily OS", "OSWorld + custom cross-platform task suite", "Pending IRB / ethics approval"],
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
    "We're proving these two halves of our thesis in two separate ways. The safety algorithm is tested against hundreds of sample commands, with no human volunteers needed at all. " +
    "The interface itself will be tested with 40 real users, once we have ethics approval to run that study."
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
    "Out of several hundred risky commands we tested, today's safety list let data get silently destroyed over 40 percent of the time — damage the user never agreed to. " +
    "Our algorithm, and a much stricter 'block everything unfamiliar' approach, both brought that down to essentially zero."
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
    "Being safe isn't enough on its own — a system that blocks everything is also 'safe', but useless, because it never actually saves anything. " +
    "Our algorithm actually recovers the lost data about 9 times out of 10; the overly-cautious approach recovers none of it, because it never even tries."
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
      "Within-subjects: 40 participants vs. their own primary OS",
      "Two populations: fluent in neither interface, fluent in both",
      "Tasks: OSWorld benchmark + custom cross-platform suite",
      "Measures: completion time, error rate, SUS, NASA-TLX, interview",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "We'll compare SynapseOS against whatever operating system each participant already uses every day. " +
    "We're deliberately recruiting two kinds of users — people comfortable with neither the command line nor a typical desktop, and people comfortable with both — because the gap between those two groups is what we actually care about measuring."
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
      { text: "Responding directly to “manage scope carefully”:\n\n", options: { bold: true, color: C.accent2, breakLine: true, fontSize: 19 } },
      { text: "Algorithm + GUI-mode launch — mandatory. Model fine-tuning + full comparative study — real, but first to shrink.", options: { color: C.background1, fontSize: 18 } },
    ],
    { x: 0.95, y: 1.9, w: 11.4, h: 1.75, fontFace: THEME.bodyFontFace, isTextBox: true, margin: 0, valign: "middle" }
  );

  s.addText(
    "A reorder of priority, not of ambition.",
    { x: 0.6, y: 4.1, w: 12.1, h: 0.8, fontFace: THEME.bodyFontFace, fontSize: 18, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "Our adviser told us, directly, to manage our scope carefully — fair, since this thesis covers a lot of ground. " +
    "Our answer: the safety algorithm and getting the interface running are non-negotiable; fine-tuning the AI model and the full user study are real plans, but the first things we'd trim if we run short on time."
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
    "The algorithm work is basically finished. The interface build is underway. " +
    "The real bottleneck is ethics approval — our paperwork is ready, but not yet submitted, so that's our top priority right now."
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
      "Already asked once, by our adviser",
      "Current answer: a prose argument — the rule table is itself a list",
      "Not yet a number — no ablation performed",
      "Plan: analyzer flag → opaque fallback → fourth corpus column",
    ], { fontSize: 17 }),
    { x: 0.9, y: 3.65, w: 11.5, h: 3.2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "We're raising this ourselves because our adviser already asked it once: how much of our result comes from the algorithm, versus just the lookup table it's built on? " +
    "Right now we can only answer that in words, not numbers — but we already have a concrete plan to measure it directly, and we're not hiding that gap."
  );
}

// ---------------------------------------------------------------------------
// 16. Expected contribution
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Expected Contribution", { placeholder: "title" });

  const stats = [
    { stat: "RQ1", label: "Composition- and resolution-aware algorithm, evaluated against two baselines", color: GOOD },
    { stat: "RQ2-3", label: "Interface layer with a reversibility-based confirmation gate and undo, over an unchanged desktop", color: THEME.colors.accent3 },
    { stat: "RQ4", label: "First controlled evidence on narrowing the CLI/GUI fluency gap", color: THEME.colors.accent2 },
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
      x: x + 0.3, y: 3.05, w: w - 0.6, h: 2.9, fontFace: THEME.bodyFontFace, fontSize: 17, color: C.background1, align: "center", valign: "top", isTextBox: true, margin: 0,
    });
  });

  s.addNotes(
    "If both halves of this project succeed, we'll have two separate wins: a safer, more useful way to catch dangerous commands, and, pending approval, the first real evidence on whether talking to a computer actually helps people who struggle with existing interfaces. " +
    "Each one holds up on its own even if the other gets delayed."
  );
}

// ---------------------------------------------------------------------------
// 17. Q&A
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "TITLE" });
  s.addText("Questions", { placeholder: "title" });
  s.addText("Thank you.", { placeholder: "subtitle" });
  s.addNotes("Thank you — we're happy to take your questions.");
}

(async () => {
  await pres.writeFile({ fileName: OUT });
  await applyTheme(OUT, THEME);
  console.log("wrote", OUT);
})();
