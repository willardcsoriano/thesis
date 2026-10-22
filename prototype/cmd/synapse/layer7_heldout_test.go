// Layer 7's held-out set (D46). The development set in layer7_corpus_test.go was
// written and tuned while the system was built against qwen2.5-coder:3b, so it
// is development data for that model and would favour it in a comparison. These
// tasks were written on 2026-10-23, before any model ran on them, and are
// frozen: pilot/layer7_heldout.sha256 holds this file's hash, and
// TestLayer7HeldOutFrozen fails on any change. Changing a task after seeing
// results would turn the set back into development data, so a change needs a
// new set under a new name, not an edit here.
//
// Same rules as the development set: three phrasings per intent (plain words,
// desktop vocabulary, shell vocabulary), a fresh fixture per run, and scoring
// on the end state, or on what the person was told for a task that changes
// nothing. Intents differ from the development set's.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func heldOutUtteranceCorpus() []utteranceTask {
	return []utteranceTask{
		{
			id: "H1", category: "file organisation",
			setup: func(t *testing.T, d string) {
				write(t, d, "docs/a.txt", "alpha")
				write(t, d, "docs/b.txt", "beta")
			},
			phrasings: [3]string{
				"pack up the docs folder into a single file I can send to someone",
				"compress the docs folder into one archive file",
				"tar czf docs.tar.gz docs",
			},
			solution: `tar czf docs.tar.gz docs`,
			verify: func(d, out string) (bool, string) {
				if !exists(d, "docs/a.txt") {
					return false, "the original folder is gone"
				}
				if anyArchiveHolds(d, "a.txt", "b.txt") {
					return true, ""
				}
				return false, "no archive here holds both files"
			},
		},
		{
			id: "H2", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "names.txt", "Pedro\nAna\nLuis\nCarmen\nBea\n")
			},
			phrasings: [3]string{
				"put the names in names.txt in alphabetical order",
				"sort the lines of names.txt alphabetically and save them back into names.txt",
				"sort -o names.txt names.txt",
			},
			solution: `sort -o names.txt names.txt`,
			verify: func(d, out string) (bool, string) {
				got := lines(d, "names.txt")
				if !slices.Equal(got, []string{"Ana", "Bea", "Carmen", "Luis", "Pedro"}) {
					return false, "names.txt is not the five names in order: " + strings.Join(got, ",")
				}
				return true, ""
			},
		},
		{
			id: "H3", category: "file search",
			setup: func(t *testing.T, d string) {
				old := time.Now().Add(-48 * time.Hour)
				for _, f := range []string{"budget.ods", "letter.txt", "plan.md"} {
					write(t, d, f, "x")
					if err := os.Chtimes(filepath.Join(d, f), old, old); err != nil {
						t.Fatal(err)
					}
				}
				write(t, d, "latest-draft.txt", "x")
			},
			phrasings: [3]string{
				"which of my files did I work on last",
				"show me the most recently modified file in this folder",
				"ls -t | head -1",
			},
			solution: `ls -t | head -1`,
			verify: func(d, out string) (bool, string) {
				if !strings.Contains(out, "latest-draft.txt") {
					return false, "latest-draft.txt was not named"
				}
				return true, ""
			},
		},
		{
			id: "H4", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "emails.txt", "ana@example.com\nluis@example.com\nana@example.com\nbea@example.com\nluis@example.com\n")
			},
			phrasings: [3]string{
				"some addresses in emails.txt are listed more than once, clean that up",
				"remove the duplicate lines from emails.txt so each address appears once",
				"sort -u -o emails.txt emails.txt",
			},
			solution: `sort -u -o emails.txt emails.txt`,
			verify: func(d, out string) (bool, string) {
				got := lines(d, "emails.txt")
				sorted := slices.Clone(got)
				slices.Sort(sorted)
				if !slices.Equal(sorted, []string{"ana@example.com", "bea@example.com", "luis@example.com"}) {
					return false, "emails.txt is not each address once: " + strings.Join(got, ",")
				}
				return true, ""
			},
		},
		{
			id: "H5", category: "file organisation",
			setup: func(t *testing.T, d string) {},
			phrasings: [3]string{
				"set up a place for my 2024 photos inside a photos folder",
				"create a folder named 2024 inside a new folder named photos",
				"mkdir -p photos/2024",
			},
			solution: `mkdir -p photos/2024`,
			verify: func(d, out string) (bool, string) {
				if fi, err := os.Stat(filepath.Join(d, "photos", "2024")); err != nil || !fi.IsDir() {
					return false, "photos/2024 is not a folder"
				}
				return true, ""
			},
		},
		{
			id: "H6", category: "file organisation",
			setup: func(t *testing.T, d string) {
				write(t, d, "backup.sh", "#!/bin/sh\necho backing up\n")
			},
			phrasings: [3]string{
				"make backup.sh something I can run",
				"make the file backup.sh executable",
				"chmod +x backup.sh",
			},
			solution: `chmod +x backup.sh`,
			verify: func(d, out string) (bool, string) {
				fi, err := os.Stat(filepath.Join(d, "backup.sh"))
				if err != nil {
					return false, "backup.sh is gone"
				}
				if fi.Mode().Perm()&0o100 == 0 {
					return false, "backup.sh is not executable by its owner"
				}
				if b, _ := os.ReadFile(filepath.Join(d, "backup.sh")); string(b) != "#!/bin/sh\necho backing up\n" {
					return false, "backup.sh was changed"
				}
				return true, ""
			},
		},
		{
			id: "H7", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "diary.txt", "woke up late\nhad coffee with Bea\nmet Ana at the station\n")
			},
			phrasings: [3]string{
				"what's the last thing I wrote in my diary",
				"show the last line of diary.txt",
				"tail -n 1 diary.txt",
			},
			solution: `tail -n 1 diary.txt`,
			verify: func(d, out string) (bool, string) {
				if !strings.Contains(out, "met Ana at the station") {
					return false, "the last entry was not shown"
				}
				return true, ""
			},
		},
		{
			id: "H8", category: "file search",
			setup: func(t *testing.T, d string) {
				for _, f := range []string{"beach.jpg", "cake.jpg", "dog.jpg", "garden.jpg", "party.jpg", "map.png", "logo.png", "notes.txt", "todo.txt"} {
					write(t, d, f, "x")
				}
			},
			phrasings: [3]string{
				"how many pictures do I have in here",
				"count the image files (.jpg and .png) in this folder",
				"ls *.jpg *.png | wc -l",
			},
			solution: `ls *.jpg *.png | wc -l`,
			verify: func(d, out string) (bool, string) {
				if !reports(out, "7") {
					return false, "the count 7 was not reported"
				}
				return true, ""
			},
		},
		{
			id: "H9", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "contacts.csv", "name,email,city\nAna,ana@example.com,Manila\nLuis,luis@example.com,Cebu\nBea,bea@example.com,Davao\n")
			},
			phrasings: [3]string{
				"make me a list of just the email addresses from my contacts, saved as emails.txt",
				"save only the email column of contacts.csv into a new file called emails.txt",
				"cut -d, -f2 contacts.csv | tail -n +2 > emails.txt",
			},
			solution: `cut -d, -f2 contacts.csv | tail -n +2 > emails.txt`,
			verify: func(d, out string) (bool, string) {
				got := lines(d, "emails.txt")
				got = slices.DeleteFunc(got, func(s string) bool { return s == "email" })
				if !slices.Equal(got, []string{"ana@example.com", "luis@example.com", "bea@example.com"}) {
					return false, "emails.txt is not the three addresses: " + strings.Join(got, ",")
				}
				if !exists(d, "contacts.csv") {
					return false, "contacts.csv is gone"
				}
				return true, ""
			},
		},
		{
			id: "H10", category: "file organisation",
			setup: func(t *testing.T, d string) {
				for _, e := range []string{"old-stuff", "temp", "unused/inner"} {
					if err := os.MkdirAll(filepath.Join(d, e), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				write(t, d, "keep/report.txt", "x")
			},
			phrasings: [3]string{
				"clear out the empty folders here",
				"delete every empty folder in this folder",
				"find . -type d -empty -delete",
			},
			solution: `find . -type d -empty -delete`,
			verify: func(d, out string) (bool, string) {
				for _, e := range []string{"old-stuff", "temp", "unused/inner"} {
					if exists(d, e) {
						return false, e + " is still there"
					}
				}
				if !exists(d, "keep/report.txt") {
					return false, "a folder with a file in it was removed"
				}
				return true, ""
			},
		},
		{
			id: "H11", category: "system monitoring",
			setup: func(t *testing.T, d string) {},
			phrasings: [3]string{
				"is there much room left on my computer",
				"show how much free disk space is available",
				"df -h .",
			},
			solution: `df -h .`,
			verify: func(d, out string) (bool, string) {
				if !reportsSize(out) {
					return false, "no amount of space was reported"
				}
				return true, ""
			},
		},
		{
			id: "H12", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "todo.txt", "call the bank\npay rent\n")
			},
			phrasings: [3]string{
				"add buy milk to the bottom of my to-do list in todo.txt",
				"append the line buy milk to the end of todo.txt",
				"echo 'buy milk' >> todo.txt",
			},
			solution: `echo 'buy milk' >> todo.txt`,
			verify: func(d, out string) (bool, string) {
				got := lines(d, "todo.txt")
				if !slices.Equal(got, []string{"call the bank", "pay rent", "buy milk"}) {
					return false, "todo.txt is not the two items plus buy milk: " + strings.Join(got, ",")
				}
				return true, ""
			},
		},
	}
}

// lines reads a fixture file as trimmed, non-empty lines; a missing file is no
// lines.
func lines(dir, name string) []string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// anyArchiveHolds reports whether some tar, gzip-compressed tar or zip file in
// dir (not inside docs/) lists every one of names, by base name. The plain
// phrasing names no archive, so any name and any of the common formats count.
func anyArchiveHolds(dir string, names ...string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		listed := archiveNames(filepath.Join(dir, e.Name()))
		all := len(listed) > 0
		for _, n := range names {
			all = all && listed[n]
		}
		if all {
			return true
		}
	}
	return false
}

// archiveNames lists the base names in a tar, tar.gz or zip file, or nothing
// for any other file.
func archiveNames(path string) map[string]bool {
	got := map[string]bool{}
	if zr, err := zip.OpenReader(path); err == nil {
		defer zr.Close()
		for _, f := range zr.File {
			got[filepath.Base(f.Name)] = true
		}
		return got
	}
	f, err := os.Open(path)
	if err != nil {
		return got
	}
	defer f.Close()
	var r io.Reader = f
	if gz, err := gzip.NewReader(f); err == nil {
		defer gz.Close()
		r = gz
	} else if _, err := f.Seek(0, io.SeekStart); err != nil {
		return got
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err != nil {
			return got
		}
		got[filepath.Base(h.Name)] = true
	}
}
