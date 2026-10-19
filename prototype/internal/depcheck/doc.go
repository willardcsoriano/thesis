// Package depcheck holds no code, only a test: it keeps distro/manifest.tsv,
// the list the SynapseOS image is assembled from, complete with respect to
// the programs the Go code runs.
//
// The test parses every Go file under prototype/ and collects the external
// programs named where a program is started or looked up: exec.Command,
// exec.CommandContext and exec.LookPath with a literal name, the argv slices
// the analysis' resolvers, the undo inverses and the sandboxes build (a
// []string literal passed to a call, or an inner slice of a [][]string
// literal, whose first element is a literal program name), absolute paths
// under /bin, /usr/bin, /sbin and /usr/sbin, and command lines handed to
// executor.Run and executor.RunIn. Each must be the probe of a manifest row,
// so a new dependency fails the test until it is listed.
//
// What the scan cannot see (a name reached through a table, a program a Go
// module runs for us) is in the test's indirect list, each entry pinned to
// the file that makes it a dependency so a stale entry fails too. Names the
// scan sees that are not dependencies are in its allowlist, each with the
// reason.
package depcheck
