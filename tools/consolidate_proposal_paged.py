#!/usr/bin/env python3
"""Consolidate Chapters 1, 2, and 3 into a Mapua University writing-guidelines compliant monograph with page-bottom footnotes using Paged.js."""
import os
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SUBMISSIONS = ROOT / "research-methods"

CH1_PATH = SUBMISSIONS / "module 2" / "submissions" / "summative-assessment-1.html"
CH2_PATH = SUBMISSIONS / "module 3" / "submissions" / "summative-assessment-2.html"
CH3_PATH = SUBMISSIONS / "module 3" / "submissions" / "summative-assessment-3.1.html"
OUT_PATH = SUBMISSIONS / "consolidated" / "SynapseOS_Proposal_Chapters_1_to_3_paged.html"
PDF_PATH = SUBMISSIONS / "consolidated" / "SynapseOS_Proposal_Chapters_1_to_3_paged.pdf"

def extract_style(html_str):
    m = re.search(r'<style[^>]*>(.*?)</style>', html_str, re.DOTALL | re.IGNORECASE)
    return m.group(0) if m else "<style></style>"

def namespace_footnotes(html_str, ch_num):
    s = re.sub(r'href="#fn(\d+)"', rf'href="#ch{ch_num}-fn\1"', html_str)
    s = re.sub(r'id="fn(\d+)"', rf'id="ch{ch_num}-fn\1"', s)
    return s

def strip_toc_blocks(html_str):
    return re.sub(r'<div class="toc-block">.*?</div>', '', html_str, flags=re.DOTALL)

def extract_appendices(html_str):
    """Pull the Appendices block out of Chapter 3's body so it can be placed after
    the consolidated References section instead of before it."""
    m = re.search(r'<div class="references">\s*<h2[^>]*id="sec-appendices"[^>]*>.*?</div>', html_str, re.DOTALL)
    if not m:
        return "", html_str
    return m.group(0), html_str[:m.start()] + html_str[m.end():]

def strip_references_section(html_str):
    """Remove the whole <div class="references">...</div> wrapper that holds Chapter
    N's own References list, including its closing tag."""
    pattern = re.compile(
        r'<div class="references">\s*<h2[^>]*id="sec-references"[^>]*>.*?</h2>'
        r'(?:\s*<div class="ref-entry"[^>]*>.*?</div>)*\s*</div>',
        re.DOTALL | re.IGNORECASE,
    )
    m = pattern.search(html_str)
    if not m:
        return html_str
    return html_str[:m.start()] + html_str[m.end():]

def extract_body_inner(html_str):
    m = re.search(r'<body[^>]*>(.*?)</body>', html_str, re.DOTALL | re.IGNORECASE)
    return m.group(1).strip() if m else html_str

def collect_references(html_list):
    refs = {}
    for html in html_list:
        for m in re.finditer(r'<div class="ref-entry"[^>]*>\s*\[(\d+)\](.*?)</div>', html, re.DOTALL):
            num = int(m.group(1))
            refs[num] = m.group(0)
    return refs

def compile_pdf():
    env = os.environ.copy()
    env['LD_LIBRARY_PATH'] = str(ROOT / "tools" / "vendor" / "libs") + os.pathsep + env.get('LD_LIBRARY_PATH', '')
    subprocess.run(
        [str(ROOT / "tools" / "venv" / "bin" / "python"), str(ROOT / "tools" / "export_pdf_paged.py"), str(OUT_PATH), str(PDF_PATH)],
        env=env,
        check=True
    )

def detect_page_numbers():
    env = os.environ.copy()
    env['LD_LIBRARY_PATH'] = str(ROOT / "tools" / "vendor" / "libs") + os.pathsep + env.get('LD_LIBRARY_PATH', '')
    txt_path = OUT_PATH.with_suffix(".probe.txt")
    subprocess.run(
        [str(ROOT / "tools" / "vendor" / "pdftotext"), "-layout", str(PDF_PATH), str(txt_path)],
        env=env,
        check=True
    )
    pages = txt_path.read_text(encoding="utf-8").split("\f")
    if txt_path.exists():
        txt_path.unlink()

    # Search for headings across the whole document
    targets = {
        'APR': ('APPROVAL PAGE', 1, len(pages)),
        'ACK': ('ACKNOWLEDGEMENT', 1, len(pages)),
        'TOC': ('TABLE OF CONTENTS', 1, len(pages)),
        'LOT': ('LIST OF TABLES', 1, len(pages)),
        'LOF': ('LIST OF FIGURES', 1, len(pages)),
        'ABS': ('ABSTRACT', 1, len(pages)),
        'CH1': ('Chapter 1', 1, len(pages)),
        'CH2': ('Chapter 2', 1, len(pages)),
        'CH3': ('Chapter 3', 1, len(pages)),
        'REF': ('REFERENCES', 1, len(pages)),
        'APP': ('Appendices', 1, len(pages))
    }
    exact_line_keys = {'APR', 'ACK', 'TOC', 'LOT', 'LOF', 'ABS', 'CH1', 'CH2', 'CH3', 'REF'}

    found = {}
    for key, (search_str, min_p, max_p) in targets.items():
        for p in range(min_p, min(max_p + 1, len(pages) + 1)):
            page_text = pages[p - 1]
            lines = [line.strip() for line in page_text.split('\n')]
            if key in exact_line_keys:
                if search_str in lines:
                    found[key] = p
                    break
            elif key == 'APP':
                if 'Appendices' in lines or 'Appendices' in page_text:
                    found[key] = p
                    break

    # Fill defaults if not found
    defaults = {
        'APR': 2, 'ACK': 3, 'TOC': 4, 'LOT': 5, 'LOF': 6, 'ABS': 7,
        'CH1': 8, 'CH2': 12, 'CH3': 24,
        'REF': 38, 'APP': 37
    }
    for k, v in defaults.items():
        if k not in found:
            found[k] = v

    return found

def main():
    ch1_raw = CH1_PATH.read_text(encoding="utf-8")
    ch2_raw = CH2_PATH.read_text(encoding="utf-8")
    ch3_raw = CH3_PATH.read_text(encoding="utf-8")

    style = extract_style(ch3_raw)
    refs_map = collect_references([ch1_raw, ch2_raw, ch3_raw])

    # Clean the body content
    ch1_body = namespace_footnotes(strip_references_section(strip_toc_blocks(extract_body_inner(ch1_raw))), 1)
    ch2_body = namespace_footnotes(strip_references_section(strip_toc_blocks(extract_body_inner(ch2_raw))), 2)
    ch3_body_raw = strip_toc_blocks(extract_body_inner(ch3_raw))
    appendices_html, ch3_body_raw = extract_appendices(ch3_body_raw)
    ch3_body = namespace_footnotes(strip_references_section(ch3_body_raw), 3)
    appendices_html = namespace_footnotes(appendices_html, 3)

    # Attach IDs for chapter headings
    ch1_body = ch1_body.replace('class="chapter-heading"', 'class="chapter-heading" id="sec-chapter1"', 1)
    ch2_body = ch2_body.replace('class="chapter-heading"', 'class="chapter-heading" id="sec-chapter2"', 1)
    ch3_body = ch3_body.replace('class="chapter-heading"', 'class="chapter-heading" id="sec-chapter3"', 1)

    # Clean up stylesheet
    style = style.replace(".toc-block {", "div.toc-block {")
    style = style.replace(".references {\n    page-break-before: always;", ".references {\n    page-break-before: avoid;")
    style = style.replace(".references {\n    page-break-before: always;\n  }", ".references {\n    page-break-before: avoid;\n  }")
    style = style.replace(".references {\n    page-break-before:always;\n  }", ".references {\n    page-break-before: avoid;\n  }")
    style = style.replace(".references { page-break-before: always; }", ".references { page-break-before: avoid; }")
    style = style.replace(".references { page-break-before:always; }", ".references { page-break-before: avoid; }")

    # Replace table styles with Mapua-compliant layout
    table_css_regex = r'/\* ── Tables ── \*/\s*table\s*\{[^}]*\}\s*th,\s*td\s*\{[^}]*\}\s*th\s*\{[^}]*\}'
    mapua_table_css = """/* ── Tables ── */
  table {
    width: 100%;
    border-collapse: collapse;
    margin: 0.6em 0 1em 0;
    font-size: 11pt;
    border-top: 1.5pt solid #000;    /* Thick top line per Section J */
    border-bottom: 1.5pt solid #000; /* Thick bottom line per Section J */
    line-height: 1.25;               /* Single space for table content */
  }

  th, td {
    border: none;                    /* No vertical lines per Section J */
    padding: 0.2cm 0.4cm;
    text-align: left;
    vertical-align: top;
  }

  th {
    font-weight: bold;
    border-bottom: 1.0pt solid #000; /* Thin line under header row */
    background: transparent;          /* No fill effects per Section J */
  }"""
    style = re.sub(table_css_regex, mapua_table_css, style, flags=re.DOTALL)

    # Insert CSS rules for page layout and Paged.js footnotes
    style = style.replace("</style>", """
  /* Base page layout: page number on the top right */
  @page {
    size: A4;
    margin: 2.54cm 2.54cm 2.54cm 3.18cm; /* 1in top/bottom/right, 1.25in left */
    @top-right {
      content: counter(page);
      font-family: "Times New Roman", Georgia, serif;
      font-size: 12pt;
    }
    @footnote {
      border-top: 0.5pt solid #555;
      padding-top: 8pt;
      margin-top: 10pt;
    }
  }

  /* Suppress top-right page number on the Title Page */
  @page :first {
    @top-right {
      content: none !important;
    }
  }

  /* Prevent double page break right after title page */
  #master-toc div.toc-block:first-of-type {
    page-break-before: avoid !important;
  }
  
  /* Prevent double page break before references */
  .references {
    page-break-before: avoid !important;
  }

  /* Paged.js Footnote Styling */
  .footnote {
    float: footnote;
    font-family: "Times New Roman", Georgia, serif;
    font-size: 10pt;
    line-height: 1.15;
    text-align: justify;
    font-weight: normal;
  }

  ::footnote-call {
    font-family: "Times New Roman", Georgia, serif;
    font-size: 8pt;
    vertical-align: super;
    line-height: 0;
  }

  ::footnote-marker {
    font-family: "Times New Roman", Georgia, serif;
    font-size: 10pt;
    margin-right: 0.5em;
    font-weight: normal;
  }
</style>""")

    # Build Master References block
    refs_html = []
    for num in sorted(refs_map.keys()):
        refs_html.append("  " + refs_map[num])
    master_refs = "\n".join(refs_html)

    # Formal Mapua University Title Page with uniform co-authors
    title_page = """
<div class="title-page" style="text-align: center; padding-top: 2cm; page-break-after: always;">
  <div style="font-size: 20pt; font-weight: bold; line-height: 1.3; margin-bottom: 3cm;">
    SynapseOS: Designing a Conversational Session Layer for the Linux Desktop
  </div>

  <div style="font-size: 14pt; margin-bottom: 0.5cm;">by</div>

  <div style="font-size: 16pt; font-weight: bold; line-height: 1.6; margin-bottom: 3.5cm;">
    Alexandra Sulit<br>
    Willard Soriano<br>
    Allyson Vivar
  </div>

  <div style="font-size: 14pt; line-height: 1.5; margin-bottom: 3.5cm;">
    A Thesis Proposal Submitted to the Department of Computer Science<br>
    in Partial Fulfillment of the Requirements for the Degree<br>
    <strong>Bachelor of Science in Computer Science</strong>
  </div>

  <div style="font-size: 14pt; line-height: 1.4;">
    Mapúa University – Makati<br>
    July 2026
  </div>
</div>
"""

    approval_page = """
<div id="approval-page" style="text-align: center; padding-top: 6cm; page-break-before: always; page-break-after: always;">
  <h1 style="font-size: 12pt; font-weight: bold; margin-bottom: 3cm; text-align: center;">APPROVAL PAGE</h1>
  <p style="text-indent: 0; text-align: center;"><em>[To be inserted upon approval.]</em></p>
</div>
"""

    acknowledgement_page = """
<div id="acknowledgement-page" style="text-align: center; padding-top: 6cm; page-break-before: always; page-break-after: always;">
  <h1 style="font-size: 12pt; font-weight: bold; margin-bottom: 3cm; text-align: center;">ACKNOWLEDGEMENT</h1>
  <p style="text-indent: 0; text-align: center;"><em>[To be inserted prior to final submission.]</em></p>
</div>
"""

    abstract_page = """
<div id="abstract-page" style="page-break-before: always; page-break-after: always;">
  <h1 style="font-size: 12pt; font-weight: bold; line-height: 2.0; margin-bottom: 30pt; text-align: center;">ABSTRACT</h1>
  <p style="line-height: 1.0; text-indent: 1.50cm;">This study designs, implements, and evaluates SynapseOS, a conversational session layer for the Linux desktop that replaces the conventional graphical session &mdash; the desktop shell, session manager, and application launcher &mdash; with a natural-language interface powered by a locally-hosted small language model (Qwen2.5-Coder-3B-Instruct).&nbsp; The system translates natural-language commands into shell operations executed through the standard Linux command-line toolchain, incorporating a reversibility-based confirmation gate for irreversible operations and an undo mechanism for recoverable ones.&nbsp; The system is evaluated through a controlled, within-subjects user study comparing task completion time, error rate, and user satisfaction between SynapseOS and each participant&rsquo;s own primary operating system across novice and power user populations (n&nbsp;=&nbsp;20).&nbsp; This evaluation methodology addresses a recognized gap in the literature, where conversational computing interfaces are rarely compared against conventional graphical workflows on the same tasks and population.</p>
  <p style="text-indent: 0; margin-top: 24pt; line-height: 2.0;"><strong>Keywords:</strong> conversational interface, session layer, natural language processing, Linux desktop, user study</p>
</div>
"""

    def make_doc_content(pages_dict):
        p_apr = pages_dict.get('APR', 2)
        p_ack = pages_dict.get('ACK', 3)
        p_toc = pages_dict.get('TOC', 4)
        p_lot = pages_dict.get('LOT', 5)
        p_lof = pages_dict.get('LOF', 6)
        p_abs = pages_dict.get('ABS', 7)
        p_ch1 = pages_dict.get('CH1', 8)
        p_ch2 = pages_dict.get('CH2', 12)
        p_ch3 = pages_dict.get('CH3', 24)
        p_ref = pages_dict.get('REF', 38)
        p_app = pages_dict.get('APP', 37)

        m_toc = f"""
<div class="toc-block">
  <p class="toc-title">TABLE OF CONTENTS</p>

  <p class="toc-entry"><a href="#title-page">TITLE PAGE</a><span class="toc-page">1</span></p>
  <p class="toc-entry"><a href="#approval-page">APPROVAL PAGE</a><span class="toc-page">{p_apr}</span></p>
  <p class="toc-entry"><a href="#acknowledgement-page">ACKNOWLEDGEMENT</a><span class="toc-page">{p_ack}</span></p>
  <p class="toc-entry"><a href="#master-toc">TABLE OF CONTENTS</a><span class="toc-page">{p_toc}</span></p>
  <p class="toc-entry"><a href="#list-of-tables">LIST OF TABLES</a><span class="toc-page">{p_lot}</span></p>
  <p class="toc-entry"><a href="#list-of-figures">LIST OF FIGURES</a><span class="toc-page">{p_lof}</span></p>
  <p class="toc-entry"><a href="#abstract-page">ABSTRACT</a><span class="toc-page">{p_abs}</span></p>

  <p class="toc-entry" style="font-weight: bold; margin-top: 0.8em;"><a href="#sec-chapter1">Chapter 1: INTRODUCTION</a><span class="toc-page">{p_ch1}</span></p>
  <p class="toc-entry level-2"><a href="#sec-gap">Gap / Opportunity</a><span class="toc-page">{p_ch1}</span></p>
  <p class="toc-entry level-2"><a href="#sec-problem">Problem Statement</a><span class="toc-page">{p_ch1 + 1}</span></p>
  <p class="toc-entry level-2"><a href="#sec-objectives">Objectives</a><span class="toc-page">{p_ch1 + 2}</span></p>
  <p class="toc-entry level-2"><a href="#sec-significance">Significance of the Study</a><span class="toc-page">{p_ch1 + 2}</span></p>
  <p class="toc-entry level-2"><a href="#sec1-1">1.1 Scope and Limitations of the Study</a><span class="toc-page">{p_ch1 + 3}</span></p>

  <p class="toc-entry" style="font-weight: bold; margin-top: 0.8em;"><a href="#sec-chapter2">Chapter 2: REVIEW OF RELATED LITERATURE</a><span class="toc-page">{p_ch2}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-1">2.1 Introduction to the Review</a><span class="toc-page">{p_ch2}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-2">2.2 Theoretical Framework</a><span class="toc-page">{p_ch2}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-3">2.3 Historical Foundations of Natural-Language Interfaces</a><span class="toc-page">{p_ch2 + 1}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-4">2.4 Natural-Language-to-Shell Translation</a><span class="toc-page">{p_ch2 + 2}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-5">2.5 Graphical and Computer-Using Agents</a><span class="toc-page">{p_ch2 + 3}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-6">2.6 Accessibility and Speech-Driven Interaction</a><span class="toc-page">{p_ch2 + 5}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-7">2.7 Evaluation Environments and Benchmarks</a><span class="toc-page">{p_ch2 + 6}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-8">2.8 Synthesis: Comparative Analysis and Research Gap</a><span class="toc-page">{p_ch2 + 7}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-9">2.9 Review of the Methodological Literature</a><span class="toc-page">{p_ch2 + 10}</span></p>
  <p class="toc-entry level-2"><a href="#sec2-10">2.10 Summary</a><span class="toc-page">{p_ch2 + 11}</span></p>

  <p class="toc-entry" style="font-weight: bold; margin-top: 0.8em;"><a href="#sec-chapter3">Chapter 3: METHODOLOGY</a><span class="toc-page">{p_ch3}</span></p>
  <p class="toc-entry level-2"><a href="#sec-conceptual-framework">Conceptual Framework</a><span class="toc-page">{p_ch3}</span></p>
  <p class="toc-entry level-2"><a href="#sec-system-env">System Environment Specifications</a><span class="toc-page">{p_ch3 + 1}</span></p>
  <p class="toc-entry level-2"><a href="#sec1">Section 1: Dataset</a><span class="toc-page">{p_ch3 + 2}</span></p>
  <p class="toc-entry level-2"><a href="#sec2">Section 2: Procedure / Experiment</a><span class="toc-page">{p_ch3 + 7}</span></p>
  <p class="toc-entry level-2"><a href="#sec3">Section 3: System Testing</a><span class="toc-page">{p_ch3 + 10}</span></p>

  <p class="toc-entry" style="font-weight: bold; margin-top: 0.8em;"><a href="#sec-master-references">REFERENCES</a><span class="toc-page">{p_ref}</span></p>
  <p class="toc-entry" style="font-weight: bold; margin-top: 0.8em;"><a href="#sec-appendices">APPENDICES</a><span class="toc-page">{p_app}</span></p>
</div>

<div class="toc-block" id="list-of-tables" style="page-break-before: always;">
  <p class="toc-title">LIST OF TABLES</p>
  <p class="toc-entry"><a href="#table-2-1">Table 2.1: Comparative analysis of surveyed systems along the four-layer pipeline</a><span class="toc-page">{p_ch2 + 7}</span></p>
  <p class="toc-entry"><a href="#table-3-1">Table 3.1: Study evaluation machines</a><span class="toc-page">{p_ch3 + 1}</span></p>
  <p class="toc-entry"><a href="#table-3-2">Table 3.2: SynapseOS minimum deployment requirements</a><span class="toc-page">{p_ch3 + 1}</span></p>
</div>

<div class="toc-block" id="list-of-figures">
  <p class="toc-title">LIST OF FIGURES</p>
  <p class="toc-entry"><a href="#figure-2-1">Figure 2.1: The four-layer interaction pipeline for language-mediated computing interfaces</a><span class="toc-page">{p_ch2}</span></p>
  <p class="toc-entry"><a href="#figure-3-1">Figure 3.1: Conceptual framework of SynapseOS</a><span class="toc-page">{p_ch3}</span></p>
</div>
"""

        return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>SynapseOS: Research Proposal (Chapters 1–3) [Paged.js Footnotes]</title>
{style}
<style>
  .chapter-break {{
    page-break-before: always;
    margin-top: 2cm;
  }}
</style>
<!-- Paged.js Configuration and Script -->
<script>
  window.PagedConfig = {{
    auto: true,
    after: (flow) => {{
      window.pagedFinished = true;
    }}
  }};
</script>
<script src="paged.polyfill.js"></script>
<script>
  class FootnoteHandler extends Paged.Handler {{
    constructor(chunker, polisher, caller) {{
      super(chunker, polisher, caller);
    }}
    beforeParsed(content) {{
      const fnRefs = content.querySelectorAll('sup.fn');
      fnRefs.forEach(ref => {{
        const link = ref.querySelector('a');
        if (!link) return;
        const href = link.getAttribute('href');
        if (!href) return;
        const fnId = href.replace('#', '');
        const fnContentEl = content.querySelector('#' + fnId);
        if (fnContentEl) {{
          const span = document.createElement('span');
          span.className = 'footnote';
          span.innerHTML = fnContentEl.innerHTML;
          ref.parentNode.insertBefore(span, ref.nextSibling);
          ref.remove();
        }}
      }});
      const footnotesSections = content.querySelectorAll('.footnotes');
      footnotesSections.forEach(section => section.remove());
    }}
    afterRendered(pages) {{
      window.pagedFinished = true;
    }}
  }}
  Paged.registerHandlers(FootnoteHandler);
</script>
</head>
<body>

<div id="title-page">
{title_page}
</div>
{approval_page}
{acknowledgement_page}

<div id="master-toc">
{m_toc}
</div>
{abstract_page}

<div class="chapter-break"></div>
{ch1_body}

<div class="chapter-break"></div>
{ch2_body}

<div class="chapter-break"></div>
{ch3_body}

<!-- ═══════════════ CONSOLIDATED REFERENCES ═══════════════ -->
<div class="chapter-break"></div>
<div class="chapter-heading" style="position: relative;">
  <div class="chapter-title" id="sec-master-references">REFERENCES</div>
</div>
<div class="references">
{master_refs}
</div>

<!-- ═══════════════ APPENDICES ═══════════════ -->
<div class="chapter-break"></div>
{appendices_html}

</body>
</html>
"""

    # --- PASS 1: Build draft and detect exact page numbers ---
    print("Executing Pass 1: Building draft to detect page offsets...")
    initial_pages = {
        'APR': 2, 'ACK': 3, 'TOC': 4, 'LOT': 5, 'LOF': 6, 'ABS': 7,
        'CH1': 8, 'CH2': 12, 'CH3': 24,
        'REF': 38, 'APP': 37
    }
    draft_html = make_doc_content(initial_pages)
    OUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    OUT_PATH.write_text(draft_html, encoding="utf-8")
    compile_pdf()
    
    # Detect exact pages
    detected_pages = detect_page_numbers()
    print("Pass 1 detected page numbers:")
    for k, v in detected_pages.items():
        print(f"  {k} -> Page {v}")

    # --- PASS 2: Re-build HTML with correct page numbers and final compile ---
    print("Executing Pass 2: Generating final compliant PDF with exact page numbering...")
    final_html = make_doc_content(detected_pages)
    OUT_PATH.write_text(final_html, encoding="utf-8")
    compile_pdf()
    print("Pass 2 complete. Master proposal PDF successfully compiled (with Paged.js page-bottom footnotes).")

if __name__ == "__main__":
    main()
