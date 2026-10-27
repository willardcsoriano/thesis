package main

// Layer 9's held-out conversations, written on 2026-10-25 before any model ran
// on them, and frozen: pilot/layer9_heldout.sha256 holds this file's hash and
// TestLayer9HeldOutFrozen fails on any edit. They are not run while the loop is
// being tuned (the development set in layer9_corpus_test.go is for that), only
// in the model comparison, so their results are evidence the tuning never saw.
// A changed set goes under a new name.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func heldOutConversationCorpus() []conversation {
	return []conversation{
		{
			id: "K1", category: "reference to a result",
			setup: func(t *testing.T, d string) {
				write(t, d, "old.txt", "first\n")
				write(t, d, "latest.txt", "second\n")
				age(t, d, "old.txt", 48*time.Hour)
				age(t, d, "latest.txt", time.Hour)
			},
			turns: []convTurn{
				{"which file here was changed most recently", `ls -t | head -1`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "latest.txt") {
						return false, "never named latest.txt"
					}
					return true, ""
				}},
				{"make a copy of it called backup.txt", `cp latest.txt backup.txt`, func(d, told string) (bool, string) {
					if contents(d, "backup.txt") != "second\n" {
						return false, "backup.txt is not a copy of latest.txt"
					}
					if !exists(d, "latest.txt") {
						return false, "latest.txt is gone"
					}
					return true, ""
				}},
			},
		},
		{
			id: "K2", category: "same again",
			setup: func(t *testing.T, d string) {
				write(t, d, "story.txt", "once upon a time\n")
				write(t, d, "poem.txt", "roses are red violets are blue\n")
			},
			turns: []convTurn{
				{"how many words are in story.txt", `wc -w < story.txt`, func(d, told string) (bool, string) {
					if !reports(told, "4") {
						return false, "did not report 4"
					}
					return true, ""
				}},
				{"and the same for poem.txt", `wc -w < poem.txt`, func(d, told string) (bool, string) {
					if !reports(told, "6") {
						return false, "did not report 6"
					}
					return true, ""
				}},
			},
		},
		{
			id: "K3", category: "correction",
			setup: func(t *testing.T, d string) {
				write(t, d, "cat.jpg", "x")
				write(t, d, "dog.jpg", "x")
				write(t, d, "list.txt", "x")
			},
			turns: []convTurn{
				{"make a folder named photos and move the jpg files into it", `mkdir photos && mv *.jpg photos/`, func(d, told string) (bool, string) {
					if !exists(d, "photos/cat.jpg") || !exists(d, "photos/dog.jpg") {
						return false, "the jpgs are not both in photos/"
					}
					if !exists(d, "list.txt") {
						return false, "list.txt was moved too"
					}
					return true, ""
				}},
				{"oops, I wanted to keep cat.jpg out of there, put that one back", `mv photos/cat.jpg .`, func(d, told string) (bool, string) {
					if !exists(d, "cat.jpg") || exists(d, "photos/cat.jpg") {
						return false, "cat.jpg is not back"
					}
					if !exists(d, "photos/dog.jpg") {
						return false, "dog.jpg left photos/ as well"
					}
					return true, ""
				}},
			},
		},
		{
			id: "K4", category: "reference to a result",
			setup: func(t *testing.T, d string) {
				write(t, d, "huge.dat", strings.Repeat("z", 300000))
				write(t, d, "large.dat", strings.Repeat("z", 200000))
				write(t, d, "tiny.txt", "x")
			},
			turns: []convTurn{
				{"show me the files here bigger than 100 kilobytes", `find . -maxdepth 1 -type f -size +100k`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "huge.dat") || !strings.Contains(told, "large.dat") {
						return false, "did not name both large files"
					}
					return true, ""
				}},
				{"delete those", `rm huge.dat large.dat`, func(d, told string) (bool, string) {
					if exists(d, "huge.dat") || exists(d, "large.dat") {
						return false, "a large file is still there"
					}
					if _, err := os.Stat(filepath.Join(d, "tiny.txt")); err != nil {
						return false, "tiny.txt was deleted too"
					}
					return true, ""
				}},
			},
		},
	}
}
