package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"synapseos/internal/oracle"
)

// Templates cover the shapes the pilot found hard (wrappers, run-time targets,
// compounds, redirects, indirection) more densely than a sample of NL2Bash does.
// They are commands with holes. Which fixture they run on and what they do to it
// are never stated here: the oracle observes that.
var (
	names = []string{"a.txt", "b.txt", "notes.txt", "app.conf", "report.docx", "data.csv", "big.log"}
	exts  = []string{"txt", "log", "o", "csv", "conf"}
	dirs  = []string{"src", "logs", "build", "backup", "tmp"}
)

const gitSetup = `git init -q && git add -A && git -c user.email=t@t -c user.name=t commit -qm init && echo changed >> a.txt && echo scratch > untracked.txt`

type tmpl struct {
	text  string
	setup string
}

var templates = []tmpl{
	// runtime targets
	{"find . -name '*.{ext}' -delete", ""},
	{"find . -name '*.{ext}' -exec rm {} \\;", ""},
	{"find . -type f -name '*.{ext}' | xargs rm", ""},
	{"find . -name '*.{ext}' -print0 | xargs -0 rm -f", ""},
	{"find . -name '*.{ext}' -exec mv {} {}.bak \\;", ""},
	{"find . -type f -exec chmod 600 {} +", ""},
	{"find . -name '*.{ext}' -exec sed -i 's/a/b/' {} \\;", ""},
	{"find {dir} -type f -exec cp {} {dir}/copy_$RANDOM \\;", ""},
	{"find . -name '*.{ext}' | xargs -I{} cp {} {}.orig", ""},
	{"ls *.{ext} | xargs -n1 gzip", ""},
	{"for f in *.{ext}; do mv \"$f\" \"$f.bak\"; done", ""},
	{"for f in *.{ext}; do : > \"$f\"; done", ""},
	{"for f in *.{ext}; do cp \"$f\" \"copy_$f\"; done", ""},
	{"for f in *.{ext}; do rm \"$f\"; done", ""},
	{"rm $(ls *.{ext})", ""},
	{"rm $(find . -name '*.{ext}')", ""},
	{"cat {name} | xargs touch", ""},
	{"find {dir} -type f | xargs rm -f", ""},
	{"find . -name '*.{ext}' -exec sh -c 'echo \"$1\"' _ {} \\;", ""},
	{"find . -mindepth 1 -maxdepth 1 -name '*.{ext}' -exec gzip {} +", ""},
	// compounds
	{"mkdir -p out && cp *.{ext} out/ && rm *.{ext}", ""},
	{"cp {name} copy.bak && rm {name}", ""},
	{"tar czf arch.tgz {dir} && rm -r {dir}", ""},
	{"tar cf {name}.tar {name}", ""},
	{"cd {dir} && rm *", ""},
	{"mv a.txt b.txt; mv notes.txt a.txt", ""},
	{"echo a > f1 && echo b >> f1", ""},
	{"cp -r {dir} {dir}2 && rm -r {dir}", ""},
	{"sort data.csv > data.csv", ""},
	{"sort data.csv -o data.csv", ""},
	{"sed 's/x/y/' data.csv > data.csv", ""},
	{"mkdir work && mv *.{ext} work/", ""},
	{"cat a.txt b.txt > all.txt && rm a.txt b.txt", ""},
	{"touch new1 new2 && rm new1", ""},
	// redirects
	{"echo hi > {name}", ""},
	{"cat a.txt b.txt > all.txt", ""},
	{"date >> notes.txt", ""},
	{": > {name}", ""},
	{"tee {name} < b.txt", ""},
	{"printf x | tee -a notes.txt", ""},
	{"ls > listing.txt", ""},
	{"echo new > {dir}/fresh.txt", ""},
	// plain
	{"rm {name}", ""},
	{"rm -f {name}", ""},
	{"rm -rf {dir}", ""},
	{"rm -r {dir}", ""},
	{"mv {name} moved.txt", ""},
	{"mv a.txt b.txt", ""},
	{"cp {name} copy.txt", ""},
	{"cp -f a.txt b.txt", ""},
	{"ln -s a.txt link.txt", ""},
	{"ln -sf a.txt b.txt", ""},
	{"chmod -R 000 {dir}", ""},
	{"chmod 755 {name}", ""},
	{"truncate -s 0 {name}", ""},
	{"shred -u {name}", ""},
	{"dd if=/dev/zero of={name} bs=1 count=4 conv=notrunc", ""},
	{"rsync -a --delete src/ backup/", ""},
	{"rm -rf ./*", ""},
	{"unlink {name}", ""},
	{"rmdir tmp", ""},
	{"install -m 755 a.txt b.txt", ""},
	{"gzip {name}", ""},
	{"gzip -k {name}", ""},
	{"bzip2 {name}", ""},
	{"touch {name}", ""},
	{"mkdir -p {dir}/deep/er", ""},
	{"cp -r {dir} {dir}_copy", ""},
	{"mv {dir} {dir}_old", ""},
	{"rm -i -f {name}", ""},
	{"rename 's/txt/md/' *.txt", ""},
	// indirection
	{"bash -c 'rm {name}'", ""},
	{"sh -c \"rm *.{ext}\"", ""},
	{"eval \"rm {name}\"", ""},
	{"env rm {name}", ""},
	{"command rm {name}", ""},
	{"nohup rm {name}", ""},
	{"time rm {name}", ""},
	{"\\rm {name}", ""},
	{"sudo rm {name}", ""},
	{"sudo rm -rf {dir}", ""},
	{"sudo chmod 600 {name}", ""},
	{"x={name}; rm $x", ""},
	{"python3 -c \"import os; os.remove('{name}')\"", ""},
	{"perl -e 'unlink \"{name}\"'", ""},
	{"awk 'BEGIN{system(\"rm {name}\")}'", ""},
	{"xargs -I{} rm {} <<< {name}", ""},
	{"echo {name} | xargs rm", ""},
	{"rm -- {name}", ""},
	// version control (working tree)
	{"git clean -fd", gitSetup},
	{"git checkout -- .", gitSetup},
	{"git reset --hard", gitSetup},
	{"git stash", gitSetup},
	{"git rm a.txt", gitSetup},
	{"git mv a.txt c.txt", gitSetup},
	{"git checkout -b feature", gitSetup},
	// read-only
	{"ls -la", ""},
	{"find . -name '*.{ext}' -print", ""},
	{"grep -r error .", ""},
	{"cat {name} | wc -l", ""},
	{"find . -name '*.{ext}' -exec cat {} \\;", ""},
	{"du -sh *", ""},
	{"find . -type f | xargs ls -l", ""},
	{"sort data.csv", ""},
	{"diff a.txt b.txt", ""},
	{"for f in *.{ext}; do echo \"$f\"; done", ""},
	{"head -n1 *.{ext}", ""},
	{"wc -l *.{ext}", ""},
	{"echo $(ls *.{ext})", ""},
	{"find . -name '*.{ext}' | xargs grep -l a", ""},
}

// External templates. Never executed.
var externalTemplates = []string{
	"ssh host 'rm -rf {dir}'", "scp {name} host:/tmp/", "curl -X DELETE http://host/api/{name}", "wget -O {name} http://host/{name}",
	"mount /dev/sdb1 /mnt", "umount /mnt", "kill -9 1234", "pkill -f server", "dd if=/dev/zero of=/dev/sda", "docker rm -f web",
	"docker system prune -af", "useradd tester", "crontab -r", "iptables -F", "mkfs.ext4 /dev/sdb1", "rsync -a {dir}/ host:/backup/",
	"sudo shutdown -h now", "sudo reboot", "nc host 9000 < {name}", "kubectl delete pod web",
}

func drawTemplates(bw string, rng *rand.Rand, seed int64, want int, rep *report) []item {
	seen := map[string]bool{}
	var cands []candidate
	for round := 0; round < 4; round++ {
		for ti, t := range templates {
			cmd := t.text
			cmd = strings.ReplaceAll(cmd, "{ext}", exts[rng.Intn(len(exts))])
			cmd = strings.ReplaceAll(cmd, "{name}", names[rng.Intn(len(names))])
			cmd = strings.ReplaceAll(cmd, "{dir}", dirs[rng.Intn(len(dirs))])
			if seen[cmd] {
				continue
			}
			seen[cmd] = true
			cands = append(cands, candidate{src: fmt.Sprintf("template:%d", ti), cmd: cmd, setup: t.setup, partition: "T"})
		}
	}
	rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })

	res := make([]*item, len(cands))
	why := make([]string, len(cands))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range cands {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			res[i], why[i] = label(bw, seed, cands[i])
		}(i)
	}
	wg.Wait()
	var out []item
	for i := range res {
		if res[i] == nil {
			rep.excluded["template: "+why[i]]++
			continue
		}
		if len(out) < want {
			out = append(out, *res[i])
		}
	}
	for _, t := range externalTemplates {
		cmd := strings.NewReplacer("{name}", names[rng.Intn(len(names))], "{dir}", dirs[rng.Intn(len(dirs))]).Replace(t)
		out = append(out, item{Partition: "E", Src: "template:external", Shape: "external", Label: "U", Command: cmd,
			Fx: oracle.Infer(cmd, 0), Note: "effects outside the filesystem; labelled by construction, never executed"})
	}
	return out
}
