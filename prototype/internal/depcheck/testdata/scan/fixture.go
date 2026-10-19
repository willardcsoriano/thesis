// Package fixture is the depcheck scan's known answer: TestScanKnownAnswer
// lists exactly the programs below that the scan must report. It is parsed,
// never built (testdata is ignored by the go tool).
package fixture

import (
	"context"
	"os/exec"

	"synapseos/internal/executor"
)

func use(...any) {}

func calls(ctx context.Context, name, arg string) {
	exec.Command("prog-command", "-x")
	exec.CommandContext(ctx, "prog-context", "-y")
	exec.LookPath("prog-lookpath")
	use([]string{"prog-argv", arg})
	use([][]string{{"prog-table", "a"}, {"prog-table2"}})
	use("/usr/bin/prog-abs", "/usr/sbin/prog-sbin")
	executor.Run("prog-shell --flag " + arg)

	// Not programs the code starts: none of these may be reported.
	exec.Command(name)
	use("/usr/bin/has space", "/usr/bin/sub/dir", "prog-plain-string", []int{1})
	use([]string{arg, "prog-not-first"})
	use([]string{"Not-A-Program-Name"})
}
