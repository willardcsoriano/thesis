package undo

import (
	"os"
	"testing"
)

// No test may write to the developer's real ~/.synapse (journal, trash,
// backups): every test in this package gets a private home. Tests that need
// their own still set one with t.Setenv.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "synapse-undo-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
