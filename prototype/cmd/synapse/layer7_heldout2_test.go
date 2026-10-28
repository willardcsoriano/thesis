// Layer 7's second held-out set (heldout2). The first held-out set
// (layer7_heldout_test.go) was used while harness changes were judged, so it
// can no longer show whether those changes generalise. These tasks were
// written on 2026-10-27 without reading how the harness tries to help the
// model, before any model ran on them, and are frozen:
// pilot/layer7_heldout2.sha256 holds this file's hash, and
// TestLayer7HeldOut2Frozen fails on any change. Changing a task after seeing
// results would turn the set into development data, so a change needs a new
// set under a new name, not an edit here.
//
// Same rules as the other sets: three phrasings per intent (plain words,
// desktop vocabulary, shell vocabulary), a fresh fixture per run, and scoring
// on the end state, or on what the person was told for a task that changes
// nothing. Intents differ from the development set's (U1-U12) and the first
// held-out set's (H1-H12).
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func heldOut2UtteranceCorpus() []utteranceTask {
	return []utteranceTask{
		{
			id: "N1", category: "archives",
			setup: func(t *testing.T, d string) {
				writeZip(t, filepath.Join(d, "holiday.zip"), map[string]string{
					"beach.jpg": "\xff\xd8\xff\xe0 beach placeholder",
					"hotel.jpg": "\xff\xd8\xff\xe0 hotel placeholder",
				})
			},
			phrasings: [3]string{
				"someone sent me holiday.zip with our trip pictures, open it up so I can see them",
				"extract all the files from holiday.zip into this folder",
				"unzip holiday.zip",
			},
			solution: `unzip holiday.zip`,
			verify: func(d, out string) (bool, string) {
				for name, body := range map[string]string{
					"beach.jpg": "\xff\xd8\xff\xe0 beach placeholder",
					"hotel.jpg": "\xff\xd8\xff\xe0 hotel placeholder",
				} {
					if !holdsFileWith(d, name, body) {
						return false, name + " was not extracted"
					}
				}
				if !exists(d, "holiday.zip") {
					return false, "holiday.zip is gone"
				}
				return true, ""
			},
		},
		{
			id: "N2", category: "file search",
			setup: func(t *testing.T, d string) {
				write(t, d, "documents/2023/tax-receipt.pdf", "%PDF-1.4 receipt placeholder")
				write(t, d, "documents/letters/thank-you.txt", "Dear Ana, thank you for the flowers.\n")
				write(t, d, "downloads/installer.bin", "binary placeholder")
				write(t, d, "pictures/cat.jpg", "\xff\xd8\xff\xe0 cat placeholder")
			},
			phrasings: [3]string{
				"I saved my tax receipt somewhere in here but forgot where, can you find it",
				"search this folder and all its subfolders for a file with receipt in its name",
				"find . -iname '*receipt*'",
			},
			solution: `find . -iname '*receipt*'`,
			verify: func(d, out string) (bool, string) {
				if !strings.Contains(out, "tax-receipt.pdf") || !strings.Contains(out, "2023") {
					return false, "the receipt's location (documents/2023) was not given"
				}
				return true, ""
			},
		},
		{
			id: "N3", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "shopping.txt", "eggs\n\nbread\n\n\nmilk\n\napples\n")
			},
			phrasings: [3]string{
				"my shopping list has gaps between the items, close them up",
				"remove the empty lines from shopping.txt",
				"sed -i '/^$/d' shopping.txt",
			},
			solution: `sed -i '/^$/d' shopping.txt`,
			verify: func(d, out string) (bool, string) {
				if got := rawLines(d, "shopping.txt"); !slices.Equal(got, []string{"eggs", "bread", "milk", "apples"}) {
					return false, "shopping.txt is not the four items without gaps: " + strings.Join(got, "|")
				}
				return true, ""
			},
		},
		{
			id: "N4", category: "counting and inspection",
			setup: func(t *testing.T, d string) {
				write(t, d, "orders.csv", "date,customer,amount\n"+
					"2026-10-01,Ana Reyes,250\n"+
					"2026-10-02,Luis Cruz,120\n"+
					"2026-10-02,Ana Reyes,90\n"+
					"2026-10-03,Bea Santos,310\n"+
					"2026-10-05,Luis Cruz,75\n"+
					"2026-10-06,Carmen Lim,180\n"+
					"2026-10-07,Ana Reyes,60\n")
			},
			phrasings: [3]string{
				"how many different customers have ordered from me? it's all in orders.csv",
				"count the distinct names in the customer column of orders.csv",
				"cut -d, -f2 orders.csv | tail -n +2 | sort -u | wc -l",
			},
			solution: `cut -d, -f2 orders.csv | tail -n +2 | sort -u | wc -l`,
			verify: func(d, out string) (bool, string) {
				if !reports(out, "4") {
					return false, "the count 4 was not reported"
				}
				return true, ""
			},
		},
		{
			id: "N5", category: "file organisation",
			setup: func(t *testing.T, d string) {
				write(t, d, "Downloads/flight-ticket.pdf", "%PDF-1.4 ticket placeholder")
				write(t, d, "Downloads/recipe.txt", "adobo: chicken, vinegar, soy sauce\n")
				write(t, d, "Downloads/song.mp3", "ID3 song placeholder")
				write(t, d, "Documents/resume.odt", "resume placeholder")
			},
			phrasings: [3]string{
				"I'm finished with my downloads, move everything in there over to my documents",
				"move all the files from the Downloads folder into the Documents folder",
				"mv Downloads/* Documents/",
			},
			solution: `mv Downloads/* Documents/`,
			verify: func(d, out string) (bool, string) {
				for _, f := range []string{"flight-ticket.pdf", "recipe.txt", "song.mp3", "resume.odt"} {
					if !exists(d, filepath.Join("Documents", f)) {
						return false, "Documents/" + f + " is missing"
					}
				}
				for _, f := range []string{"flight-ticket.pdf", "recipe.txt", "song.mp3"} {
					if exists(d, filepath.Join("Downloads", f)) {
						return false, "Downloads/" + f + " is still there: copied, not moved"
					}
				}
				return true, ""
			},
		},
		{
			id: "N6", category: "archives",
			setup: func(t *testing.T, d string) {
				writeTarGz(t, filepath.Join(d, "backup.tar.gz"), map[string]string{
					"backup/report.odt":     "report placeholder",
					"backup/budget.ods":     "budget placeholder",
					"backup/photos/kid.jpg": "\xff\xd8\xff\xe0 kid placeholder",
				})
			},
			phrasings: [3]string{
				"what's packed inside backup.tar.gz",
				"list the files contained in the archive backup.tar.gz",
				"tar tzf backup.tar.gz",
			},
			solution: `tar tzf backup.tar.gz`,
			verify: func(d, out string) (bool, string) {
				for _, n := range []string{"report.odt", "budget.ods", "kid.jpg"} {
					if !strings.Contains(out, n) {
						return false, n + " was not named"
					}
				}
				return true, ""
			},
		},
		{
			id: "N7", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "chapter1.txt", "It was a dark and quiet night.\nThe village slept.\n")
				write(t, d, "chapter2.txt", "Morning came with rain.\nNobody went out.\n")
			},
			phrasings: [3]string{
				"join chapter1.txt and chapter2.txt together into one file called book.txt, first chapter first",
				"combine chapter1.txt and chapter2.txt into a new file named book.txt, in that order",
				"cat chapter1.txt chapter2.txt > book.txt",
			},
			solution: `cat chapter1.txt chapter2.txt > book.txt`,
			verify: func(d, out string) (bool, string) {
				want := []string{"It was a dark and quiet night.", "The village slept.", "Morning came with rain.", "Nobody went out."}
				if got := lines(d, "book.txt"); !slices.Equal(got, want) {
					return false, "book.txt is not chapter 1 then chapter 2: " + strings.Join(got, "|")
				}
				if !exists(d, "chapter1.txt") || !exists(d, "chapter2.txt") {
					return false, "a chapter file is gone"
				}
				return true, ""
			},
		},
		{
			id: "N8", category: "counting and inspection",
			setup: func(t *testing.T, d string) {
				write(t, d, "budget.ods", "budget placeholder")
				write(t, d, "notes.txt", "call the plumber\n")
				when := time.Date(2025, time.March, 14, 10, 30, 0, 0, time.Local)
				if err := os.Chtimes(filepath.Join(d, "budget.ods"), when, when); err != nil {
					t.Fatal(err)
				}
			},
			phrasings: [3]string{
				"when did I last change budget.ods",
				"show the date budget.ods was last modified",
				"stat -c %y budget.ods",
			},
			solution: `stat -c %y budget.ods`,
			verify: func(d, out string) (bool, string) {
				if !tellsDate(out, 2025, time.March, 14) {
					return false, "the date 14 March 2025 was not given"
				}
				return true, ""
			},
		},
		{
			id: "N9", category: "file search",
			setup: func(t *testing.T, d string) {
				write(t, d, "report.odt", "report placeholder")
				write(t, d, "notes/meeting.txt", "agenda: budget\n")
				write(t, d, "notes/ideas.txt", "garden shed\n")
				write(t, d, "photos/trip.jpg", "\xff\xd8\xff\xe0 trip placeholder")
				write(t, d, "invoice.pdf", "%PDF-1.4 invoice placeholder")
				age(t, d, "report.odt", time.Hour)
				age(t, d, "notes/meeting.txt", 2*time.Hour)
				for _, f := range []string{"notes/ideas.txt", "photos/trip.jpg", "invoice.pdf"} {
					age(t, d, f, 10*24*time.Hour)
				}
			},
			phrasings: [3]string{
				"which files have I worked on since yesterday? look inside the folders too",
				"list the files in this folder and its subfolders that were modified in the last 24 hours",
				"find . -type f -mtime -1",
			},
			solution: `find . -type f -mtime -1`,
			verify: func(d, out string) (bool, string) {
				for _, n := range []string{"report.odt", "meeting.txt"} {
					if !strings.Contains(out, n) {
						return false, n + " was not named"
					}
				}
				for _, n := range []string{"ideas.txt", "trip.jpg", "invoice.pdf"} {
					if strings.Contains(out, n) {
						return false, n + ", untouched for ten days, was named as well"
					}
				}
				return true, ""
			},
		},
		{
			id: "N10", category: "file organisation",
			setup: func(t *testing.T, d string) {
				write(t, d, "logs/printer.log", "paper jam\n")
				write(t, d, "logs/sync.log", "synced 12 files\n")
				write(t, d, "logs/backup.log", "backup ok\n")
				write(t, d, "logs/updates.log", "3 updates installed\n")
				write(t, d, "settings.conf", "theme=dark\n")
				age(t, d, "logs/printer.log", 30*24*time.Hour)
				age(t, d, "logs/sync.log", 15*24*time.Hour)
				age(t, d, "logs/backup.log", 2*24*time.Hour)
				age(t, d, "logs/updates.log", time.Hour)
				age(t, d, "settings.conf", 30*24*time.Hour)
			},
			phrasings: [3]string{
				"clean out the logs folder: throw away any log that's more than a week old",
				"delete the files in the logs folder that were last modified more than 7 days ago",
				"find logs -type f -mtime +7 -delete",
			},
			solution: `find logs -type f -mtime +7 -delete`,
			verify: func(d, out string) (bool, string) {
				for _, f := range []string{"logs/printer.log", "logs/sync.log"} {
					if exists(d, f) {
						return false, f + " is older than a week and still there"
					}
				}
				for _, f := range []string{"logs/backup.log", "logs/updates.log", "settings.conf"} {
					if !exists(d, f) {
						return false, f + " was deleted but should have been kept"
					}
				}
				return true, ""
			},
		},
		{
			id: "N11", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "prices.csv", "item;price;qty\nrice;52.50;2\neggs;8.00;12\ncooking oil;95.75;1\n")
			},
			phrasings: [3]string{
				"prices.csv has semicolons between the columns, change them to commas so my spreadsheet opens it right",
				"replace every semicolon in prices.csv with a comma",
				"sed -i 's/;/,/g' prices.csv",
			},
			solution: `sed -i 's/;/,/g' prices.csv`,
			verify: func(d, out string) (bool, string) {
				want := []string{"item,price,qty", "rice,52.50,2", "eggs,8.00,12", "cooking oil,95.75,1"}
				if got := rawLines(d, "prices.csv"); !slices.Equal(got, want) {
					return false, "prices.csv is not the same rows with commas: " + strings.Join(got, "|")
				}
				return true, ""
			},
		},
		{
			id: "N12", category: "system monitoring",
			setup: func(t *testing.T, d string) {},
			phrasings: [3]string{
				"how much memory does my computer have free right now",
				"show how much RAM is free and in use",
				"free -h",
			},
			solution: `free -h`,
			verify: func(d, out string) (bool, string) {
				if !reportsSize(out) {
					return false, "no amount was reported"
				}
				lower := strings.ToLower(out)
				words := strings.FieldsFunc(lower, func(r rune) bool { return r < 'a' || r > 'z' })
				if !strings.Contains(lower, "mem") && !slices.Contains(words, "ram") {
					return false, "nothing said the amount was memory"
				}
				return true, ""
			},
		},
	}
}

// rawLines reads a fixture file as its lines exactly, blank ones included,
// without the final newline; a missing file is no lines. Unlike lines, it sees
// the blank lines that N3 removes and any stray whitespace an edit leaves.
func rawLines(dir, name string) []string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil
	}
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// holdsFileWith reports whether a regular file named name with exactly body
// sits anywhere under dir. Where an archive's files land (here or in a
// folder of the archive's name) is the extracting tool's choice, and neither
// the plain nor the desktop phrasing pins it.
func holdsFileWith(dir, name, body string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() || e.Name() != name {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && string(b) == body {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// tellsDate reports whether told names the given day in a form date, ls or
// stat print, or a person would write: 2025-03-14, "Mar 14 2025",
// "14 March 2025" and the like.
func tellsDate(told string, year int, month time.Month, day int) bool {
	s := strings.ToLower(strings.Join(strings.Fields(told), " "))
	if strings.Contains(s, time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")) {
		return true
	}
	if !strings.Contains(s, strconv.Itoa(year)) {
		return false
	}
	words := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '.' })
	long, short := strings.ToLower(month.String()), strings.ToLower(month.String()[:3])
	dd := strconv.Itoa(day)
	for i := 0; i+1 < len(words); i++ {
		a, b := words[i], words[i+1]
		if ((a == long || a == short) && b == dd) || (a == dd && (b == long || b == short)) {
			return true
		}
	}
	return false
}

// writeZip writes a zip archive at path holding files, by name.
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeTarGz writes a gzip-compressed tar archive at path holding files, by
// name.
func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		body := files[name]
		h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), ModTime: time.Now(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
