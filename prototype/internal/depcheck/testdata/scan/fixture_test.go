package fixture

import (
	"os/exec"
	"testing"
)

// In a test file only what the test itself starts or looks up counts; its
// argv slices and paths are commands under analysis.
func TestFixture(t *testing.T) {
	exec.Command("prog-test-command")
	exec.LookPath("prog-test-lookpath")
	use([]string{"prog-test-argv"})
	use("/usr/bin/prog-test-abs")
}
