#!/bin/bash
# Regenerates prototype/playground/sandbox/ — a disposable, synthetic file
# tree for manually exercising synapse's file-search/organization and
# text-processing task categories without touching real files. Safe to
# delete and rerun any time; nothing here is real data. The "system &
# process monitoring" and "package management" sample categories aren't
# filesystem-based, so there's no fixture for them here — real system
# state already covers those.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/sandbox"

rm -rf "$ROOT"
mkdir -p "$ROOT/home/user/Documents" "$ROOT/home/user/Desktop" "$ROOT/home/user/Downloads"

# --- file search & organization ---

# Two PDFs inside the last 7 days, one outside it — lets you eyeball
# whether a "find pdfs modified in the last 7 days" command returns
# exactly the right two.
echo "stub" >"$ROOT/home/user/Documents/report_draft.pdf"
echo "stub" >"$ROOT/home/user/Documents/notes.pdf"
echo "stub" >"$ROOT/home/user/Documents/invoice_2026_01.pdf"
touch -d "2 days ago" "$ROOT/home/user/Documents/report_draft.pdf"
touch -d "5 days ago" "$ROOT/home/user/Documents/notes.pdf"
touch -d "40 days ago" "$ROOT/home/user/Documents/invoice_2026_01.pdf"

# Screenshots on Desktop with no Screenshots/ folder yet, plus one
# decoy non-screenshot file — lets you test "move every screenshot into a
# folder called Screenshots" and check whether the decoy gets left alone.
echo "stub" >"$ROOT/home/user/Desktop/Screenshot_2026-07-08.png"
echo "stub" >"$ROOT/home/user/Desktop/Screenshot_2026-07-11.png"
echo "stub" >"$ROOT/home/user/Desktop/vacation_photo.jpg"

echo "stub" >"$ROOT/home/user/Downloads/random_archive.tar.gz"

# --- text & data processing ---

cat >"$ROOT/home/user/Documents/access.log" <<'EOF'
INFO  2026-07-10 09:00:12 request served in 12ms
ERROR 2026-07-10 09:02:41 connection refused
INFO  2026-07-10 09:03:05 request served in 8ms
ERROR 2026-07-10 09:07:19 timeout waiting for upstream
INFO  2026-07-10 09:08:00 request served in 15ms
ERROR 2026-07-10 09:09:44 500 internal server error
EOF

printf 'name\tage\tcity\nAda\t36\tLondon\nGrace\t85\tNew York\n' >"$ROOT/home/user/Documents/data.txt"

echo "playground regenerated at $ROOT"
find "$ROOT" -type f | sort
