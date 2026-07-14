package main

import "testing"

func TestCleanCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare command", "ls -la", "ls -la"},
		{"surrounding whitespace", "  ls -la  \n", "ls -la"},
		{"triple backtick fence no lang", "```\nls -la\n```", "ls -la"},
		{"triple backtick fence with lang", "```bash\nls -la\n```", "ls -la"},
		{"inline single backticks", "`ls -la`", "ls -la"},
		{"inline single backticks with whitespace", "  `ls -la`  ", "ls -la"},
		{"unsupported sentinel untouched", "UNSUPPORTED", "UNSUPPORTED"},
		{"single backtick alone is not stripped", "`", "`"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanCommand(tc.in); got != tc.want {
				t.Errorf("cleanCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
