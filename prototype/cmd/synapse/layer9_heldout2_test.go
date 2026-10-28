package main

// Layer 9's second held-out conversations (heldout2), written on 2026-10-27
// without reading how the harness tries to help the model, before any model
// ran on them, and frozen: pilot/layer9_heldout2.sha256 holds this file's
// hash and TestLayer9HeldOut2Frozen fails on any edit. They exist because the
// first held-out set (layer9_heldout_test.go) was used while harness changes
// were judged; these measure, once, whether those changes generalise. A
// changed set goes under a new name.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func heldOut2ConversationCorpus() []conversation {
	return []conversation{
		{
			id: "Q1", category: "reference to a place",
			setup: func(t *testing.T, d string) {
				write(t, d, "billing/2025/invoice-march.pdf", "%PDF-1.4 march invoice placeholder")
				write(t, d, "billing/2025/invoice-april.pdf", "%PDF-1.4 april invoice placeholder")
				write(t, d, "billing/2025/receipt.png", "\x89PNG receipt placeholder")
				write(t, d, "billing/2024/summary.txt", "total paid: 14200\n")
				if err := os.MkdirAll(filepath.Join(d, "Desktop"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			turns: []convTurn{
				{"where is my file invoice-march.pdf", `find . -name invoice-march.pdf`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "billing/2025") {
						return false, "did not say it is in billing/2025"
					}
					return true, ""
				}},
				{"what else is in there?", `ls billing/2025`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "invoice-april.pdf") || !strings.Contains(told, "receipt.png") {
						return false, "did not name the other files in billing/2025"
					}
					return true, ""
				}},
				{"copy the march one to my Desktop folder", `cp billing/2025/invoice-march.pdf Desktop/`, func(d, told string) (bool, string) {
					if contents(d, "Desktop/invoice-march.pdf") != "%PDF-1.4 march invoice placeholder" {
						return false, "Desktop/invoice-march.pdf is not a copy of the march invoice"
					}
					if !exists(d, "billing/2025/invoice-march.pdf") {
						return false, "the march invoice left billing/2025: moved, not copied"
					}
					return true, ""
				}},
			},
		},
		{
			id: "Q2", category: "correction",
			setup: func(t *testing.T, d string) {
				write(t, d, "essay.txt", "Why I like the sea.\n")
				write(t, d, "outline.txt", "1. intro\n2. the sea\n")
			},
			turns: []convTurn{
				{"rename essay.txt to essay-final.txt", `mv essay.txt essay-final.txt`, func(d, told string) (bool, string) {
					if contents(d, "essay-final.txt") != "Why I like the sea.\n" || exists(d, "essay.txt") {
						return false, "essay.txt was not renamed to essay-final.txt"
					}
					return true, ""
				}},
				{"that was a mistake, give it its old name back", `mv essay-final.txt essay.txt`, func(d, told string) (bool, string) {
					if contents(d, "essay.txt") != "Why I like the sea.\n" || exists(d, "essay-final.txt") {
						return false, "the essay is not called essay.txt again"
					}
					return true, ""
				}},
				{"now make a copy of it called essay-backup.txt", `cp essay.txt essay-backup.txt`, func(d, told string) (bool, string) {
					if contents(d, "essay-backup.txt") != "Why I like the sea.\n" {
						return false, "essay-backup.txt is not a copy of the essay"
					}
					if !exists(d, "essay.txt") || !exists(d, "outline.txt") {
						return false, "a file is gone"
					}
					return true, ""
				}},
			},
		},
		{
			id: "Q3", category: "building on earlier turns",
			setup: func(t *testing.T, d string) {
				write(t, d, "guests.txt", "Ana\nLuis\nBea\nCarmen\n")
				write(t, d, "menu.txt", "pancit\nlechon\n")
			},
			turns: []convTurn{
				{"show me who's on my guest list in guests.txt", `cat guests.txt`, func(d, told string) (bool, string) {
					for _, n := range []string{"Ana", "Luis", "Bea", "Carmen"} {
						if !strings.Contains(told, n) {
							return false, n + " was not shown"
						}
					}
					return true, ""
				}},
				{"add Rosa to the end of it", `echo Rosa >> guests.txt`, func(d, told string) (bool, string) {
					got := lines(d, "guests.txt")
					if strings.Join(got, ",") != "Ana,Luis,Bea,Carmen,Rosa" {
						return false, "guests.txt is not the four guests then Rosa: " + strings.Join(got, ",")
					}
					if contents(d, "menu.txt") != "pancit\nlechon\n" {
						return false, "menu.txt was changed"
					}
					return true, ""
				}},
				{"how many people are on it now?", `wc -l < guests.txt`, func(d, told string) (bool, string) {
					if !reports(told, "5") {
						return false, "did not report 5"
					}
					return true, ""
				}},
			},
		},
		{
			id: "Q4", category: "undo",
			setup: func(t *testing.T, d string) {
				write(t, d, "screenshot-1.png", "\x89PNG one")
				write(t, d, "screenshot-2.png", "\x89PNG two")
				write(t, d, "wallpaper.png", "\x89PNG wallpaper")
			},
			turns: []convTurn{
				{"put the screenshots into a new folder called screenshots", `mkdir screenshots && mv screenshot-*.png screenshots/`, func(d, told string) (bool, string) {
					if !exists(d, "screenshots/screenshot-1.png") || !exists(d, "screenshots/screenshot-2.png") {
						return false, "the screenshots are not both in screenshots/"
					}
					if !exists(d, "wallpaper.png") {
						return false, "wallpaper.png was moved too"
					}
					return true, ""
				}},
				{"undo that", `mv screenshots/screenshot-*.png .`, func(d, told string) (bool, string) {
					for _, f := range []string{"screenshot-1.png", "screenshot-2.png"} {
						if !exists(d, f) || exists(d, "screenshots/"+f) {
							return false, f + " is not back where it was"
						}
					}
					return true, ""
				}},
				{"how many png pictures are here now?", `ls *.png | wc -l`, func(d, told string) (bool, string) {
					if !reports(told, "3") {
						return false, "did not report 3"
					}
					return true, ""
				}},
			},
		},
	}
}
