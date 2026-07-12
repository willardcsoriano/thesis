#!/usr/bin/env python3
"""Export a thesis chapter HTML file to PDF via a vendored, self-contained Chromium,
waiting for Paged.js pagination to finish.

Usage:
    tools/venv/bin/python tools/export_pdf_paged.py <input.html> [output.pdf]
"""
import os
import sys
from pathlib import Path

TOOLS_DIR = Path(__file__).resolve().parent
VENDOR_DIR = TOOLS_DIR / "vendor"
CHROMIUM_BIN = VENDOR_DIR / "chromium" / "chrome"
LIBS_DIR = VENDOR_DIR / "libs"
FONTS_DIR = VENDOR_DIR / "fonts"

FONTCONFIG_TEMPLATE = """<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <dir>{fonts_dir}</dir>
  <cachedir>{fonts_dir}/cache</cachedir>

  <match target="pattern">
    <test name="family"><string>Times New Roman</string></test>
    <edit name="family" mode="assign" binding="same"><string>Liberation Serif</string></edit>
  </match>
  <match target="pattern">
    <test name="family"><string>Georgia</string></test>
    <edit name="family" mode="assign" binding="same"><string>Liberation Serif</string></edit>
  </match>
  <match target="pattern">
    <test name="family"><string>Arial</string></test>
    <edit name="family" mode="assign" binding="same"><string>Liberation Sans</string></edit>
  </match>

  <alias>
    <family>serif</family>
    <prefer><family>Liberation Serif</family></prefer>
  </alias>
  <alias>
    <family>sans-serif</family>
    <prefer><family>Liberation Sans</family></prefer>
  </alias>
  <alias>
    <family>monospace</family>
    <prefer><family>Liberation Mono</family></prefer>
  </alias>
</fontconfig>
"""


def write_fontconfig() -> None:
    """Regenerate fonts.conf with an absolute <dir>, so font resolution works
    regardless of the invoking process's working directory. A bare
    FONTCONFIG_PATH with no fonts.conf fails to load any config at all and
    silently falls back to an arbitrary font (observed: DejaVu Sans, ignoring
    every font-family in the source CSS) - this is what actually makes
    Times New Roman resolve instead of that fallback."""
    (FONTS_DIR / "cache").mkdir(exist_ok=True)
    (FONTS_DIR / "fonts.conf").write_text(FONTCONFIG_TEMPLATE.format(fonts_dir=FONTS_DIR))


def export(html_path: Path, pdf_path: Path) -> None:
    write_fontconfig()
    env = os.environ.copy()
    env["LD_LIBRARY_PATH"] = str(LIBS_DIR) + os.pathsep + env.get("LD_LIBRARY_PATH", "")
    env["FONTCONFIG_PATH"] = str(FONTS_DIR)

    os.environ.update(env)

    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=str(CHROMIUM_BIN),
            args=["--no-sandbox", "--disable-gpu", "--allow-file-access-from-files"],
        )
        page = browser.new_page()
        page.on("console", lambda msg: print(f"BROWSER CONSOLE: {msg.text}", file=sys.stderr, flush=True))
        page.on("pageerror", lambda exc: print(f"BROWSER ERROR: {exc}", file=sys.stderr, flush=True))
        page.on("requestfailed", lambda req: print(f"BROWSER REQUEST FAILED: {req.url} - {req.failure}", file=sys.stderr, flush=True))
        page.goto(f"file://{html_path.resolve()}")

        # Wait for Paged.js to finish its dynamic pagination layout
        page.wait_for_function("window.pagedFinished === true", timeout=30000)

        # Call Page.printToPDF directly via CDP for generateDocumentOutline.
        cdp = page.context.new_cdp_session(page)
        result = cdp.send(
            "Page.printToPDF",
            {
                "printBackground": True,
                "preferCSSPageSize": True,
                "generateDocumentOutline": True,
            },
        )

        import base64

        pdf_path.write_bytes(base64.b64decode(result["data"]))
        browser.close()


def find_page(pdf_path: Path, needle: str) -> int | None:
    """Return the 1-indexed page number where `needle` first appears, or None."""
    import re
    import subprocess

    env = os.environ.copy()
    env["LD_LIBRARY_PATH"] = str(LIBS_DIR) + os.pathsep + env.get("LD_LIBRARY_PATH", "")
    txt_path = pdf_path.with_suffix(".probe.txt")
    subprocess.run(
        [str(VENDOR_DIR / "pdftotext"), "-layout", str(pdf_path), str(txt_path)],
        env=env,
        check=True,
    )
    pages = txt_path.read_text().split("\f")
    txt_path.unlink()
    norm_needle = re.sub(r"\s+", " ", needle)
    for i, page in enumerate(pages, start=1):
        if norm_needle in re.sub(r"\s+", " ", page):
            return i
    return None


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)
    html_in = Path(sys.argv[1]).resolve()
    pdf_out = Path(sys.argv[2]).resolve() if len(sys.argv) > 2 else html_in.with_suffix(".pdf")
    export(html_in, pdf_out)
    print(f"Wrote {pdf_out} ({pdf_out.stat().st_size} bytes)")
