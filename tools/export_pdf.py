#!/usr/bin/env python3
"""Export a thesis chapter HTML file to PDF via a vendored, self-contained Chromium.

Usage:
    tools/venv/bin/python tools/export_pdf.py <input.html> [output.pdf]

Everything this script needs (Chromium binary, its shared libraries, fonts)
lives in tools/vendor/ inside the repo. Nothing is installed system-wide and
nothing depends on the host machine beyond a working Python 3 interpreter.
"""
import os
import sys
from pathlib import Path

TOOLS_DIR = Path(__file__).resolve().parent
VENDOR_DIR = TOOLS_DIR / "vendor"
CHROMIUM_BIN = VENDOR_DIR / "chromium" / "chrome"
LIBS_DIR = VENDOR_DIR / "libs"
FONTS_DIR = VENDOR_DIR / "fonts"


def export(html_path: Path, pdf_path: Path) -> None:
    env = os.environ.copy()
    env["LD_LIBRARY_PATH"] = str(LIBS_DIR) + os.pathsep + env.get("LD_LIBRARY_PATH", "")
    # Point fontconfig at just the vendored fonts so Liberation/DejaVu are found
    # even though no system fontconfig config exists in this environment.
    env["FONTCONFIG_PATH"] = str(FONTS_DIR)

    os.environ.update(env)

    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=str(CHROMIUM_BIN),
            args=["--no-sandbox", "--disable-gpu"],
        )
        page = browser.new_page()
        page.goto(f"file://{html_path.resolve()}")

        # page.pdf() doesn't yet expose generateDocumentOutline in Playwright's
        # stable API, so call Page.printToPDF directly via CDP for the extra flag.
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
