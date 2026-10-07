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
// Sizing and notes policy (2026-10-07, revised again same day): body text sized
// for reading from the back of a defense room — titles 34-44pt, body 18-20pt,
// card text no smaller than 15pt. On-slide text is kept to short phrases and
// fragments, not full sentences — nobody reads prose off a slide. The content a
// full sentence would have carried moves to the speaker notes instead, written
// as 3-5 sentences of formal, academic, spoken prose: a presenter's script with
// enough context to orient a listener, not a one-line cue and not a transcript
// to read verbatim. Where a slide's content genuinely split into two separate
// points too large for one script, it was split into two slides rather than
// compressed into one dense note.
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
    "Good [morning/afternoon], panel. We present the Thesis 1 proposal for SynapseOS, a conversational interface layer for personal computing, developed to let a person operate a Linux desktop in ordinary language rather than through memorized commands or a fixed menu of GUI actions. " +
    "This defense addresses the research plan and methodology set out in Chapters 1 through 3; the comparative user study itself has not yet been conducted, so our results today are necessarily preliminary, not final. " +
    "We will walk through the research gap motivating this work, our two independent research contributions, how each will be evaluated, our current progress against the proposed timeline, and, finally, one open question we want to raise ourselves before the panel does."
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
    "The command line can, in principle, express nearly any operation a computer supports, but only for someone fluent enough to write the syntax. " +
    "The graphical desktop removed that syntactic barrier, but replaced it with a different one: a user can only do what the interface's designers anticipated and built a control for. " +
    "Large language models have reopened a research thread that is, in fact, decades old — the Berkeley UNIX Consultant demonstrated a working natural-language interface to Unix as early as 1988 — but with substantially greater capability than was available then. " +
    "What remains missing, despite this renewed interest, is an implemented and empirically evaluated system that places a conversational layer over the entire Linux desktop session, rather than over the shell alone or a single application; that is precisely the gap this study addresses."
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
    "This statement captures the thesis's central premise: a computer's functional capability does not change between the command line and the graphical desktop, but a person's ability to reach that capability does, depending entirely on which interface they happen to be fluent in. " +
    "SynapseOS proposes a third interface — natural language — specifically because it is the one mode of expression most people already possess, regardless of technical background. " +
    "Chapters 1 through 3 of this proposal exist to argue that such an interface can be built, and, more importantly, that its value can be measured rather than merely asserted."
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
    "These four research questions were deliberately reordered following an earlier review, in which our adviser posed a pointed test: if the conversational interface were removed entirely, would any research contribution survive? For the manuscript as it then stood, the honest answer was no. " +
    "In response, we restructured the proposal so that the recoverability algorithm now leads as RQ1, standing as an independent, corpus-evaluated contribution that requires no human participants at all. Research Questions 2 through 4 remain the interface questions, and these are what the 40-participant comparative study is designed to answer. " +
    "Our four research objectives mirror these questions one-to-one, so satisfying each objective directly answers its corresponding question."
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
    "This four-layer pipeline — Input, Reasoning, Action, and Operating-System Integration — is the organizing framework developed in Chapter 2's review of the literature. It proved diagnostic in practice: nearly every surveyed system's principal limitation could be located at exactly one of these four layers, which is what makes the framework useful rather than merely descriptive. " +
    "SynapseOS instantiates the pipeline directly — natural language enters at Input, a locally-hosted small language model reasons over it, the resulting shell commands execute at the Action layer, and the system spans the full graphical session rather than a single application at the Operating-System Integration layer. " +
    "The recoverability algorithm that answers RQ1 lives specifically within the Action layer: it is the mechanism that decides, before anything executes, whether a command is safe to run outright or must first be confirmed."
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
    "This slide is a placeholder for a recorded, verified terminal session, which we will substitute once captured. What it will show, concretely, is a plain-English request typed by a user, the shell command SynapseOS generates in response, the confirmation gate intervening before anything irreversible occurs, and the resulting output. " +
    "We include this here deliberately, ahead of the algorithm's technical details, to make one point plain before anything else: this system already runs, today, on real hardware, and is not merely a design on paper."
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
    "Allowing a language model to act on a real filesystem introduces a risk the interface layer alone cannot resolve: a generated command may destroy data before its author has any opportunity to review it. " +
    "The conventional safeguard — matching a command against an enumerated list of known-dangerous patterns — judges the command by its surface form, its name, rather than by what it will actually do, so a disguised or simply unfamiliar variant of a dangerous operation passes through undetected. " +
    "The naive alternative, refusing every command the list does not recognize, is safe in the narrow sense but practically useless: it protects nothing and interrupts the user constantly. " +
    "What this study proposes instead is a third approach — an algorithm that understands a command's effects well enough, before execution, to know specifically what it will change."
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
    "The algorithm's claim is deliberately narrow, and that narrowness is itself a design decision rather than a limitation we are apologizing for. It claims exactly two abilities: composition through wrappers, meaning it sees through constructs such as find -exec, xargs, shell loops, command and process substitution, pipelines, and sudo, treating earlier segments of a line as visible context for later ones; and resolution of run-time targets, meaning that where a target is computed only at execution time, a read-only dry run establishes the concrete targets in advance, so a capture plan can be built before anything actually runs. " +
    "Commands with explicit, statically-named targets remain the responsibility of the existing pattern list, which already handled them adequately in our pilot work. Anything the analysis does not model is simply treated as unknown, and unknown fails closed by design — the system asks for confirmation and captures nothing, rather than guessing. " +
    "We want to be explicit that we are not claiming to have solved recoverability in general; we are claiming these two specific abilities, and measuring them against baselines."
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
    "We report the algorithm study and the user study as two genuinely independent contributions, because they are established through entirely separate evidence. The algorithm study requires no human participants at all: it is evaluated against a held-out corpus of generated commands, with ground truth established by sandboxed execution rather than human judgment, compared across the existing pattern list, that same list made to fail closed, and our algorithm. " +
    "The user study, by contrast, follows a classical within-subjects design, comparing each of 40 participants against the operating system they already use daily, with task performance measured through OSWorld and a custom cross-platform suite, and subjective experience measured through the System Usability Scale and the NASA Task Load Index. " +
    "That second study is the one still awaiting institutional ethics approval before it can proceed."
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
    "Out of the 405 commands in our held-out corpus that touch something outside the filesystem or lose data outright, the existing pattern list allows silent data loss in 41.2 percent of cases — a figure that, in a real deployment, represents irreversible harm the user was never even asked to accept. " +
    "Both the fail-closed list and our algorithm reduce that figure to exactly zero. This equivalence is itself an important, somewhat counterintuitive finding: on the silent-loss metric alone, the two safe approaches are statistically indistinguishable, which tells us directly that the verdict a system reaches is not, by itself, the contribution worth claiming."
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
    "Silent-loss safety is necessary but not sufficient — a system that simply refuses everything it does not recognize is equally safe by that measure, yet protects nothing. This second chart is where the approaches actually separate: of the 325 commands in our corpus that do lose something recoverable, the fail-closed list restores none of them, by construction, since it never attempts capture at all. " +
    "Our algorithm, by contrast, restores 89.5 percent of those losses, with every restoration independently verified by executing the command in a sandbox and comparing the resulting filesystem tree. " +
    "We want to be transparent that this result comes from a single held-out round with a single annotator; a second annotator and a larger corpus remain open work, tracked explicitly in our documentation rather than left implicit."
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
    "The comparative study follows a within-subjects design, meaning every participant serves as their own control, evaluated against whichever operating system they already use daily — Windows, macOS, or Linux. We deliberately recruit two distinct populations: individuals fluent in neither the command line nor the graphical desktop, and individuals fluent in both. " +
    "The gap between these two populations, rather than the raw performance of either group in isolation, is our primary outcome of interest, because it is the most direct test of our central hypothesis. " +
    "Task performance is measured using the OSWorld benchmark together with a custom cross-platform task suite we developed, while subjective experience is captured through the System Usability Scale, the NASA Task Load Index, and a semi-structured post-study interview."
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
    "Our adviser's own written feedback on an earlier draft instructed us, in those exact words, to manage our scope carefully — a fair concern, given that this proposal combines algorithm development, corpus construction, interface engineering, and a controlled user study within a single undergraduate thesis. " +
    "Our response is a written, explicit priority ordering rather than a vague reassurance: the recoverability algorithm and the GUI-mode session launch, which together constitute the distribution we intend to ship, are treated as the mandatory deliverable. Model fine-tuning details and the full forty-participant comparative study remain real, planned work, but they are explicitly the first components we would scale back if our timeline comes under pressure. " +
    "This ordering reflects priority, not ambition — we are not abandoning either track, only stating plainly which one yields first."
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
    "As of today, the algorithm track is essentially complete, with round seven standing as our reported, frozen result. The build track — the runtime and the GUI-mode session launch — remains actively in progress. " +
    "The genuine bottleneck is ethics: our study instruments are fully drafted, but the institutional review board application itself has not yet been submitted. Because IRB turnaround is the least controllable variable in our entire timeline, we are prioritizing its submission now, in parallel with remaining build work, rather than sequencing it afterward."
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
    "We would rather raise this question ourselves than have it put to us cold, because our adviser has in fact already asked it, in an earlier review, and it deserves a direct answer rather than an evasive one. At present, our answer is a prose argument rather than a measured number: we contend that the per-command rule table underlying the algorithm is itself simply a list, comparable in kind to the baseline we compare against, and that what we actually claim credit for is what the algorithm builds on top of that table — composition through wrappers, and resolution of run-time targets. " +
    "We have not yet isolated the rule table's own contribution as an explicit ablation, and we say so plainly rather than let the panel discover the gap on their own. " +
    "Our concrete plan to close it: add a flag to the analyzer that routes all wrapper and composition handling to an opaque fallback instead of resolving through it, run that configuration as a fourth comparison column against the same held-out corpus, and report whatever delta results — a scoped, well-defined next step, not an open-ended promise."
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
    "Should both evaluations proceed as designed, SynapseOS will have produced two genuinely independent contributions. The first is a recoverability algorithm, measurably safer and more useful than the pattern-matching approach it extends, evaluated entirely without human participants. The second, pending institutional approval, is the first controlled empirical evidence addressing whether a conversational interface actually narrows the gap between users fluent in the command line and users fluent in neither existing interface. " +
    "Critically, each contribution stands on its own merit even if the other is delayed or scaled back — that mutual independence was a deliberate decision made early in this project's design, not a fortunate accident discovered later."
  );
}

// ---------------------------------------------------------------------------
// 17. Q&A
// ---------------------------------------------------------------------------
{
  const s = pres.addSlide({ masterName: "TITLE" });
  s.addText("Questions", { placeholder: "title" });
  s.addText("Thank you.", { placeholder: "subtitle" });
  s.addNotes("We welcome the panel's questions, and thank you for your time and consideration.");
}

(async () => {
  await pres.writeFile({ fileName: OUT });
  await applyTheme(OUT, THEME);
  console.log("wrote", OUT);
})();
