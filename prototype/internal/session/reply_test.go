package session

import "testing"

func TestLastReply(t *testing.T) {
	c := New()
	if a, ch := c.LastReply(); a != "" || ch {
		t.Fatalf("empty session: %q %v", a, ch)
	}
	c.AppendAnswered("what's the biggest file", []Step{{Command: "ls -S"}}, "  big.bin  ")
	if a, ch := c.LastReply(); a != "big.bin" || ch {
		t.Errorf("after a look-up: %q %v", a, ch)
	}
	c.AppendAnswered("move it", []Step{{Command: "mv big.bin old/", Changed: []string{"old/big.bin"}}}, "Moved.")
	if a, ch := c.LastReply(); a != "Moved." || !ch {
		t.Errorf("after a change: %q %v", a, ch)
	}
	c.Append("again", []Step{{Command: "true"}})
	if a, _ := c.LastReply(); a != "" {
		t.Errorf("a turn without a reply kept the one before: %q", a)
	}
	c.AppendAnswered("long", []Step{{Command: "true"}}, string(make([]byte, 1000)))
	if a, _ := c.LastReply(); len(a) > 300 {
		t.Errorf("the reply is not bounded: %d bytes", len(a))
	}
	c.Clear()
	if a, _ := c.LastReply(); a != "" {
		t.Errorf("Clear kept the reply: %q", a)
	}
}
