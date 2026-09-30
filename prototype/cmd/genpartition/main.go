// Command genpartition asks the deployed model for a command for each NL2Bash
// description and writes the commands it produced, ready for corpusgen -from.
//
// The point is a partition of model-generated commands: the recoverability analysis
// is about commands a language model writes, and NL2Bash's own commands are written by
// people. The prompt and generation options are the runtime's (cmd/synapse
// loopSystemPrompt, first step, temperature 0), copied here because that package is
// main. If the runtime's prompt changes, change it here too.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"

	"synapseos/internal/ollama"
)

const loopSystemPrompt = `You are the command translator for a Linux system running Debian 13 (Trixie).
Output the single next bash command needed to make progress on the task.

The prompt may contain two clearly separate sections. Do not confuse them:
- "Earlier in this session" is background from PREVIOUS, ALREADY-FINISHED tasks. Use it only to understand what words like "it", "that", or "the file" refer to. It is NEVER progress on the current task.
- "Steps already run" is progress on the CURRENT task, and only those steps count toward finishing it.

Rules:
- Output ONLY the command. No explanation, no commentary, no markdown code fences.
- If the CURRENT task has no steps run yet, always output a command — never DONE, no matter what earlier tasks did.
- If the steps already run for the CURRENT task have fully accomplished it, output exactly: DONE
- Combine steps into one line with pipes or && where you reasonably can, but if a step depends on seeing the result of a previous command first, propose only that next step.
- Prefer standard, widely available utilities.
- Commands run in the working directory given below. Words like "here", "this folder", "this directory", or "the current folder" refer to THAT directory: write a relative path or ".", never an absolute path to somewhere else.
- NEVER output a placeholder path. /path/to/folder, /path/to/file, /your/directory and similar are not real paths and the command will fail. If the task does not name a path, it means the working directory — use a relative path.
- Do not substitute a well-known system directory for one the task did not mention. "the log files here" means log files in the working directory, not /var/log.
- If the task cannot be done with a shell command at all, output exactly: UNSUPPORTED`

var (
	srcRe = regexp.MustCompile(`nl2bash:(\d+)`)
	fence = regexp.MustCompile("(?s)^```[a-z]*\n?(.*?)\n?```$")
)

func main() {
	nl := flag.String("nl", "pilot/nl2bash_all.nl", "NL2Bash descriptions, one per line")
	exclude := flag.String("exclude", "pilot/corpus.jsonl,pilot/corpus2.jsonl,pilot/corpus3.jsonl", "corpora whose NL2Bash indices to skip")
	n := flag.Int("n", 300, "how many descriptions to ask about")
	seed := flag.Int64("seed", 20260924, "random seed")
	model := flag.String("model", "qwen2.5-coder:3b", "model name")
	url := flag.String("url", "http://127.0.0.1:11435", "Ollama base URL")
	out := flag.String("out", "pilot/generated6.tsv", "output: index<TAB>model<TAB>command")
	flag.Parse()

	raw, err := os.ReadFile(*nl)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lines := strings.Split(string(raw), "\n")
	used := map[int]bool{}
	for _, p := range strings.Split(*exclude, ",") {
		b, _ := os.ReadFile(strings.TrimSpace(p))
		for _, m := range srcRe.FindAllStringSubmatch(string(b), -1) {
			var i int
			fmt.Sscan(m[1], &i)
			used[i] = true
		}
	}
	rng := rand.New(rand.NewSource(*seed))
	client := ollama.New(*url)
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	opts := map[string]any{"temperature": 0, "num_ctx": 8192}
	done, skipped := 0, 0
	for _, i := range rng.Perm(len(lines)) {
		if done >= *n {
			break
		}
		task := strings.TrimSpace(lines[i])
		if task == "" || used[i] {
			continue
		}
		prompt := "Working directory: /home/user/project\n\nTask: " + task + "\nWhat command should run next?"
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		resp, err := client.Generate(ctx, *model, loopSystemPrompt, prompt, opts)
		cancel()
		if err != nil {
			skipped++
			fmt.Fprintf(os.Stderr, "skip %d: %v\n", i, err)
			continue
		}
		cmd := strings.TrimSpace(resp.Response)
		if m := fence.FindStringSubmatch(cmd); m != nil {
			cmd = strings.TrimSpace(m[1])
		}
		if cmd == "" || cmd == "UNSUPPORTED" || cmd == "DONE" || strings.Contains(cmd, "\n") {
			skipped++
			continue
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", i, *model, cmd)
		w.Flush()
		done++
		if done%25 == 0 {
			fmt.Fprintf(os.Stderr, "%d commands\n", done)
		}
	}
	fmt.Fprintf(os.Stderr, "wrote %d commands, %d descriptions gave nothing usable\n", done, skipped)
}
