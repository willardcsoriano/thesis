#!/usr/bin/env bash
# Recreates the manual-test playground from scratch: ordinary personal files that the
# stories in STORIES.md clean up, delete, and restore. Safe to run any number of times;
# it only ever touches the playground folder itself.
set -euo pipefail
P="${1:-$HOME/Documents/SynapseOS-Playground}"
case "$P" in */SynapseOS-Playground) ;; *) echo "refusing: path must end in SynapseOS-Playground" >&2; exit 1 ;; esac
rm -rf -- "$P"
# A throwaway test home also gets a clean undo journal, so each run starts with nothing to
# undo but itself. Never touched in a real home directory.
case "$HOME" in /tmp/synapse-*) rm -rf -- "$HOME/.synapse" ;; esac
mkdir -p "$P/OldPhotos" "$P/Projects"
cd "$P"
for f in "Screenshot 2026-09-14 101211.png" "Screenshot 2026-09-20 184502.png" \
         "beach.jpg" "beach copy.jpg" "sunset.jpg" "sunset copy.jpg" \
         "photos-backup.zip" "installer.zip" \
         "OldPhotos/2019-birthday.jpg" "OldPhotos/2019-graduation.jpg"; do
  printf 'sample image data for %s\n' "$f" > "$f"
done
printf 'Dear diary,\nToday I tested my own thesis.\n' > "secret diary.txt"
printf 'Resume draft v1\n' > "resume draft.docx"
printf 'Thesis draft v3\n' > "thesis draft.docx"
printf 'Thesis FINAL - do not delete\n' > "thesis final.docx"
printf 'Groceries: eggs, rice, coffee\n' > "notes.txt"
printf '#!/bin/sh\necho hello\n' > "Projects/run.sh"
printf 'print("hi")\n' > "Projects/app.py"
chmod 755 "Projects/run.sh"
chmod 644 "Projects/app.py"
echo "playground ready: $P"
find "$P" -type f | sed "s#^$P/#  #" | sort
