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
// Sizing and notes policy (2026-10-08, light-mode + script revision): light
// background per a colleague's defense-room feedback — dark text on white/light
// card fills, not the other way around. `THEME.colors.dk1`/`lt1` are OOXML
// theme-slot names (dk1 = "dark 1", lt1 = "light 1"), not literal shade
// descriptions — dk1 holds our page background (white) and lt1 holds our
// primary text color (near-black); the names are fixed by the pptxgenjs/OOXML
// theme schema, not by what's actually light or dark. Titles 34-44pt, body
// 18-20pt, card text no smaller than 15pt. On-slide text stays short phrases
// and fragments — nobody reads prose off a slide.
//
// Speaker notes are the full literal script — presenters read them verbatim,
// not as cues to paraphrase. That means: (1) every slide with a quote, a
// table, a chart, or a set of bullets needs a bridging sentence that tells
// the audience what to look at before explaining it ("On screen is...",
// "Read the statement above:", "This chart shows..."), because nobody will
// improvise that bridge live; (2) plain, concrete words over abstraction or
// jargon, written for a listener who knows nothing about this codebase;
// (3) still concise — 2-4 sentences depending on how much the slide needs
// introduced, never a paragraph, never purely decorative transitions.
const pptxgen = require("pptxgenjs");
const { applyTheme } = require("/home/willard/.claude/skills/synced/52694aae-ba96-4669-9afa-6d301ea140af_9d0ddd6e-8877-4575-a3ce-ad3e2d35de48/pptx/scripts/apply_theme.js");

const OUT = "/home/willard/projects/thesis/thesis 1/defense/SynapseOS_Proposal_Defense.pptx";

const THEME = {
  name: "SynapseOS Light",
  headFontFace: "Courier New",
  bodyFontFace: "Calibri",
  colors: {
    dk1: "F6F8FA", // page background (OOXML slot name only - holds our light color)
    lt1: "1A1F26", // primary text (OOXML slot name only - holds our dark color)
    dk2: "FFFFFF", // card / panel background - white, popping off the gray page
    lt2: "57606A", // muted secondary text
    accent1: "1A7F37", // green - primary accent
    accent2: "9A6700", // amber - caution / open-question accent
    accent3: "0969DA", // blue - secondary data
    accent4: "CF222E", // red - danger / unsafe
    accent5: "57606A",
    accent6: "1A1F26",
    hlink: "0969DA",
    folHlink: "57606A",
  },
};

const DANGER = "CF222E";
const NEUTRAL = "6E7781";
const GOOD = "1A7F37";
const GRIDLINE = "D0D7DE";
const BORDER = "D0D7DE";

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
    "Good [morning/afternoon]. We're presenting our Thesis 1 proposal for SynapseOS, software that lets someone control a computer by typing plain English instead of memorized commands. " +
    "Right now, we've built and tested the core safety technology behind it, and we already have a working prototype people can try. " +
    "What's ahead is finishing the full interface, clearing ethics review, and running a study comparing it against the computer people already use. " +
    "Today we're presenting that plan for your approval — not final results, since that comparison study hasn't happened yet."
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
      "Graphical desktop — bound to what its creator thought to include",
      "LLMs reopen the thread (Berkeley UNIX Consultant, 1988) with far greater capability",
      "Missing: an implemented, evaluated system for the whole desktop session",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "The command line can do almost anything on a computer, but only if you know the exact words to type. " +
    "A graphical interface is easier, but it only lets you do what its creator thought to build in — nothing more. " +
    "Nobody has yet built and tested a system that lets you control a whole desktop just by talking to it normally — that's the gap we're filling."
  );
}

// ---------------------------------------------------------------------------
// 3. Problem statement
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Problem Statement", { placeholder: "title" });
  s.addShape(pres.ShapeType.roundRect, {
    x: 1.1, y: 2.5, w: 11.1, h: 2.2, rectRadius: 0.1,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1 },
  });
  s.addText(
    "Not everyone can get a computer to do what it's capable of — it depends on which interface you already know.",
    {
      x: 1.5, y: 2.5, w: 10.3, h: 2.2,
      fontFace: THEME.bodyFontFace, fontSize: 23, color: C.background1,
      align: "left", valign: "middle", isTextBox: true, margin: 0, italic: true,
    }
  );
  s.addNotes(
    "Here's the core problem we're solving — please read the statement on screen. " +
    "In plain terms: a computer can do almost anything, but only people who already know the command line, or a particular app's menus, can actually reach that power. " +
    "Everyone else is stuck with less than the machine can really do."
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
      "RQ2 — Direct a full desktop in ordinary language",
      "RQ3 — Confirm and recover from mistakes, including ones the user approved",
      "RQ4 — Narrow the fluency gap between CLI and GUI non-experts",
    ], { fontSize: 19 }),
    { placeholder: "body" }
  );
  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 6.25, w: 12.1, h: 0.75, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { color: BORDER, width: 1 },
  });
  s.addText(
    [
      { text: "RQ1 leads on purpose ", options: { bold: true, color: C.accent1 } },
      { text: "— two independent contributions, not one subordinate to the other.", options: { color: C.background2 } },
    ],
    { x: 0.9, y: 6.25, w: 11.5, h: 0.75, fontFace: THEME.bodyFontFace, fontSize: 15, valign: "middle", isTextBox: true, margin: 0 }
  );
  s.addNotes(
    "On screen are the four questions guiding this research. " +
    "We reordered them after an earlier review, when our adviser asked whether anything would survive if we removed the chat interface entirely — so the safety algorithm now leads as its own question, and the other three, about the interface itself, come after."
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
    "On screen is simply the four-step flow behind a system like this: understand what you asked, decide what to do, act on the computer, then fit into your existing desktop. " +
    "SynapseOS follows this same flow, and the safety check we'll show next happens right before anything actually runs."
  );
}

// ---------------------------------------------------------------------------
// 6. What it looks like
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("What It Looks Like", { placeholder: "title" });
  s.addImage({
    path: "media/what-it-looks-like.gif",
    x: 1.35, y: 1.85, w: 10.6, h: 4.82,
  });
  s.addNotes(
    "This is a real recording, three requests in a row, on this laptop — a demo folder in our Documents called SynapseOS-Demo, which we can open and show you directly if you'd like. " +
    "Plain English in, a real command and a real answer out, each time: listing the folder, renaming a file, counting photos."
  );
}

// ---------------------------------------------------------------------------
// 7. What the algorithm sees
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("What The Algorithm Sees", { placeholder: "title" });
  s.addText(
    "Not the chat interface — a tool we built for the algorithm itself, reasoning about a real command:",
    { x: 0.6, y: 1.6, w: 12.1, h: 0.55, fontFace: THEME.bodyFontFace, fontSize: 18, color: C.background2, isTextBox: true, margin: 0 }
  );
  s.addImage({
    path: "media/algorithm-trace.png",
    x: 1.35, y: 2.3, w: 10.6, h: 1.48,
  });
  s.addText(
    [
      { text: "Sees through find -exec ", options: { bold: true, color: C.accent1 } },
      { text: "→ resolves the real files it would touch → labels it recoverable, with a capture plan.", options: { color: C.background1 } },
    ],
    { x: 0.6, y: 4.1, w: 12.1, h: 0.6, fontFace: THEME.bodyFontFace, fontSize: 16, valign: "middle", isTextBox: true, margin: 0 }
  );
  s.addNotes(
    "This is the algorithm itself running, not the friendly chat interface you just saw — a debugging tool we built called effexplain. " +
    "Give it a command, and it shows exactly how it reasons: it sees through the find-exec wrapper, works out the real files it would touch, and labels the result recoverable, with a plan to capture those files first. " +
    "This is what's actually happening underneath the conversation from a moment ago."
  );
}

// ---------------------------------------------------------------------------
// 8. The algorithm - the problem
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
  s.addText(
    "The algorithm itself is real code in our repository, not a formula on paper — prototype/internal/effects.",
    { x: 0.6, y: 5.6, w: 12.1, h: 0.6, fontFace: THEME.bodyFontFace, fontSize: 14, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );
  s.addNotes(
    "If an AI types commands for you, it might delete something important before you even notice. " +
    "Today's safety checks only look at whether a command looks familiar, not at what it actually does — so you either let unfamiliar commands through and risk damage, or block everything and get interrupted constantly. " +
    "Our algorithm lives in real code, not a formula — in our repository, under internal/effects — and it actually reads what a command would do before deciding."
  );
}

// ---------------------------------------------------------------------------
// 9. The algorithm - what it claims
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("The Algorithm (RQ1) — What It Claims", { placeholder: "title" });

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 1.5, w: 5.85, h: 1.85, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1.2 },
  });
  s.addText("Composition through wrappers", {
    x: 0.85, y: 1.65, w: 5.35, h: 0.5, fontFace: "Courier New", fontSize: 16, bold: true, color: C.accent1, isTextBox: true, margin: 0,
  });
  s.addText(
    "find -exec, xargs, loops, substitution, pipelines, sudo — earlier segments visible to later ones",
    { x: 0.85, y: 2.2, w: 5.35, h: 1.1, fontFace: THEME.bodyFontFace, fontSize: 15, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 6.75, y: 1.5, w: 5.85, h: 1.85, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1.2 },
  });
  s.addText("Resolution of run-time targets", {
    x: 7.0, y: 1.65, w: 5.35, h: 0.5, fontFace: "Courier New", fontSize: 16, bold: true, color: C.accent1, isTextBox: true, margin: 0,
  });
  s.addText(
    "Targets computed at run time (rm $(ls *.log), find -delete, xargs rm) resolved via a read-only dry run before execution",
    { x: 7.0, y: 2.2, w: 5.35, h: 1.1, fontFace: THEME.bodyFontFace, fontSize: 15, color: C.background1, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 3.55, w: 12.0, h: 0.65, rectRadius: 0.06,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent2, width: 1 },
  });
  s.addText(
    [
      { text: "Deliberately narrow: ", options: { bold: true, color: C.accent2 } },
      { text: "unmodeled commands are unknown → fail closed, ask, capture nothing.", options: { color: C.background1 } },
    ],
    { x: 0.9, y: 3.55, w: 11.4, h: 0.65, fontFace: THEME.bodyFontFace, fontSize: 15, valign: "middle", isTextBox: true, margin: 0 }
  );

  s.addImage({
    path: "media/safety-gate.png",
    x: 2.75, y: 4.35, w: 7.8, h: 2.29,
  });

  s.addNotes(
    "Our algorithm does two things. It sees through tricks that hide what a command really does, like chaining several commands together. " +
    "And when it can't know a command's target in advance, it does a safe test run first to find out. " +
    "Anything it doesn't understand, it simply stops and asks — it never guesses."
  );
}

// ---------------------------------------------------------------------------
// 10. Evaluation design - two independent studies
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
      lines: ["40 participants, within-subjects", "Each person vs. their own daily OS", "OSWorld + custom cross-platform task suite", "Awaiting clearance to begin"],
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
    "This research has two separate parts: the safety algorithm, and the chat interface. " +
    "We test the algorithm against hundreds of sample commands, with no human volunteers needed at all. " +
    "We test the interface with 40 real users, comparing it to the computer they already use — once that study is cleared to run."
  );
}

// ---------------------------------------------------------------------------
// 11. Silent data loss - chart
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
      valGridLine: { color: GRIDLINE, size: 1 },
      catGridLine: { style: "none" },
      showLegend: false,
    }
  );

  s.addText(
    "Out of 405 commands that touch something outside the filesystem or lose data.",
    { x: 0.6, y: 6.75, w: 12.1, h: 0.4, fontFace: THEME.bodyFontFace, fontSize: 13, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "This chart shows how often each approach silently destroys data the user never agreed to lose. " +
    "The method most tools use today — a list of known-dangerous commands — fails over 40 percent of the time. " +
    "Our algorithm brings that down to essentially zero, matching the safest possible approach."
  );
}

// ---------------------------------------------------------------------------
// 12. Restoration coverage - chart
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
      valGridLine: { color: GRIDLINE, size: 1 },
      catGridLine: { style: "none" },
      showLegend: false,
    }
  );

  s.addText(
    "Single-annotator corpus, one held-out round — a second annotator and a larger sample are still open (open-problems.md, row 27).",
    { x: 0.6, y: 6.75, w: 12.1, h: 0.4, fontFace: THEME.bodyFontFace, fontSize: 13, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "This chart shows how often each approach actually recovers data after something is deleted. " +
    "Being safe isn't enough by itself — a system that blocks everything is 'safe' too, but saves nothing. " +
    "Our algorithm recovers the lost data about 9 times out of 10; the overly-cautious approach recovers none of it, because it never tries to."
  );
}

// ---------------------------------------------------------------------------
// 13. Interface evaluation plan
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
    "On screen is how we'll test the interface with real people: comparing SynapseOS against whatever computer and operating system each person already uses every day. " +
    "We're recruiting two kinds of users on purpose — people who struggle with both existing options, and people fluent in both — because the gap between those two groups is what we actually care about measuring."
  );
}

// ---------------------------------------------------------------------------
// 14. Scope & sequencing
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
      { text: "Algorithm + interface launch — mandatory. Model fine-tuning + full comparative study — real, but first to shrink.", options: { color: C.background1, fontSize: 18 } },
    ],
    { x: 0.95, y: 1.9, w: 11.4, h: 1.75, fontFace: THEME.bodyFontFace, isTextBox: true, margin: 0, valign: "middle" }
  );

  s.addText(
    "We are managing a research algorithm and an entirely new interface at the same time. Period.",
    { x: 0.6, y: 4.1, w: 12.1, h: 0.8, fontFace: THEME.bodyFontFace, fontSize: 18, italic: true, color: C.background2, isTextBox: true, margin: 0 }
  );

  s.addShape(pres.ShapeType.roundRect, {
    x: 0.6, y: 5.0, w: 12.1, h: 1.0, rectRadius: 0.08,
    fill: { color: THEME.colors.dk2 }, line: { color: THEME.colors.accent1, width: 1 },
  });
  s.addText(
    [
      { text: "Also advised: ", options: { bold: true, color: C.accent1 } },
      { text: "rent a cloud testing machine instead of buying new hardware — keeps costs down.", options: { color: C.background1 } },
    ],
    { x: 0.95, y: 5.0, w: 11.4, h: 1.0, fontFace: THEME.bodyFontFace, fontSize: 17, valign: "middle", isTextBox: true, margin: 0 }
  );

  s.addNotes(
    "What's on screen is our adviser's own feedback, word for word: manage your scope carefully. " +
    "Fair — we are managing a research algorithm and an entirely new interface at the same time. Period. " +
    "Our answer: the algorithm and getting the interface running are non-negotiable; fine-tuning the model and the full user study are real plans, but the first things we'd trim if time runs short. " +
    "Our adviser also advised renting a cloud machine for testing instead of buying new hardware, which keeps our costs down."
  );
}

// ---------------------------------------------------------------------------
// 15. Current status
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("Current Status", { placeholder: "title" });

  const rows = [
    { label: "Algorithm (RQ1)", status: "Essentially done — round 7 reported", color: GOOD },
    { label: "Interface build", status: "In progress", color: THEME.colors.accent2 },
    { label: "Testing machine", status: "Solved — renting a cloud VM instead of buying hardware", color: GOOD },
  ];
  let y = 1.9;
  rows.forEach((r) => {
    s.addShape(pres.ShapeType.roundRect, {
      x: 0.6, y, w: 12.1, h: 1.3, rectRadius: 0.06,
      fill: { color: THEME.colors.dk2 }, line: { color: BORDER, width: 1 },
    });
    s.addShape(pres.ShapeType.ellipse, { x: 0.95, y: y + 0.54, w: 0.26, h: 0.26, fill: { color: r.color }, line: { type: "none" } });
    s.addText(r.label, { x: 1.4, y: y + 0.15, w: 4.9, h: 1.0, fontFace: "Courier New", fontSize: 18, bold: true, color: C.background1, valign: "middle", isTextBox: true, margin: 0 });
    s.addText(r.status, { x: 6.4, y: y + 0.15, w: 6.1, h: 1.0, fontFace: THEME.bodyFontFace, fontSize: 17, color: C.background2, valign: "middle", isTextBox: true, margin: 0 });
    y += 1.5;
  });

  s.addNotes(
    "Here's where we actually stand. The algorithm is essentially done — round seven is our reported result. The interface build is underway. " +
    "Testing used to be a real bottleneck, since destructive testing needs a disposable machine we're willing to break — but thanks to our adviser's advice, we're renting a cloud machine instead of buying new hardware, so that's solved, not blocking us."
  );
}

// ---------------------------------------------------------------------------
// 16. Known open question
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
    "Before you ask — here's a question our adviser already raised in an earlier review, so we're putting it on screen and answering it ourselves. Please read the quote above. " +
    "Our answer today is words, not a number yet: the table underneath the algorithm is itself just a list, and what we're actually claiming credit for is what's built on top of it. " +
    "We already have a concrete plan to turn that into an actual measured number."
  );
}

// ---------------------------------------------------------------------------
// 17. Expected contribution
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
      fill: { color: THEME.colors.dk2 }, line: { color: BORDER, width: 1 },
    });
    s.addText(st.stat, {
      x, y: 1.95, w, h: 1.05, fontFace: THEME.headFontFace, fontSize: 32, bold: true, color: st.color, align: "center", isTextBox: true, margin: 0,
    });
    s.addText(st.label, {
      x: x + 0.3, y: 3.05, w: w - 0.6, h: 2.9, fontFace: THEME.bodyFontFace, fontSize: 17, color: C.background1, align: "center", valign: "top", isTextBox: true, margin: 0,
    });
  });

  s.addNotes(
    "As soon as this project succeeds, it delivers two separate results: a safer, more useful way to catch dangerous commands, and the first real evidence on whether talking to a computer actually helps people who struggle with existing interfaces. " +
    "Each result stands on its own, even if the other is delayed."
  );
}

// ---------------------------------------------------------------------------
// 18. What's next
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "CONTENT" });
  s.addText("What's Next", { placeholder: "title" });
  s.addText(
    bullets([
      "Spin up the rented testing machine",
      "Finish the interface build",
      "Run the study with real participants",
      "Return with results, not just a plan",
    ]),
    { placeholder: "body" }
  );
  s.addNotes(
    "Once this proposal is approved, here's what happens next: we set up our rented testing machine, finish building the interface, and run the full study with real participants. " +
    "The next time we present, we'll be showing actual results instead of a plan."
  );
}

// ---------------------------------------------------------------------------
// 19. Thank you / Q&A
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "TITLE" });
  s.addText("Thank You", { placeholder: "title" });
  s.addText(
    [
      { text: "SynapseOS — Thesis 1 Proposal Defense\n\n", options: { breakLine: true, fontSize: 18, color: C.background2 } },
      { text: "Alexandra Sulit · Willard Soriano · Steven Evian Lozano\n", options: { breakLine: true, fontSize: 16, color: C.background2 } },
      { text: "Department of Computer Science, Mapúa University – Makati\n\n", options: { breakLine: true, fontSize: 16, color: C.background2 } },
      { text: "We welcome your questions.", options: { fontSize: 20, color: C.background1 } },
    ],
    { placeholder: "subtitle" }
  );
  s.addNotes(
    "Thank you for your time and consideration today. We're glad to take any questions you have about the plan, the algorithm, or anything else we've covered."
  );
}

(async () => {
  await pres.writeFile({ fileName: OUT });
  await applyTheme(OUT, THEME);
  console.log("wrote", OUT);
})();
