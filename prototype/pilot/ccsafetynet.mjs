// Scores the corpus with cc-safety-net (github.com/kenryu42/cc-safety-net, MIT), an
// open-source pre-execution guard used with Claude Code, Codex, Gemini CLI and other
// agents, as an external baseline. It calls the package's own checkCommand, unmodified,
// once per corpus command per preset (standard, strict, paranoid), from the command's
// own starting directory as laid out by `extbaselines -fixtures DIR`. Nothing is run.
//
// Run it from a directory where cc-safety-net@2.6.1 is installed:
//   npm install --ignore-scripts cc-safety-net@2.6.1
//   node ccsafetynet.mjs pilot/corpus7.jsonl FIXTURE_DIR >> pilot/external_baselines7.jsonl
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { checkCommand } from 'cc-safety-net/api';

const [corpusPath, fixtures] = process.argv.slice(2);
const levels = ['standard', 'strict', 'paranoid'];
for (const line of readFileSync(corpusPath, 'utf8').split('\n')) {
  if (!line.trim()) continue;
  const r = JSON.parse(line);
  const command = r.cmd_fx || r.command;
  const cwd = join(fixtures, r.id);
  process.env.HOME = join(cwd, 'home');
  for (const level of levels) {
    process.env.CC_SAFETY_NET_LEVEL = level;
    let res;
    try {
      res = checkCommand({ command, cwd });
    } catch (e) {
      res = { kind: 'deny', reason: `checker threw: ${e}`, ruleId: 'error' };
    }
    process.stdout.write(JSON.stringify({
      id: r.id, system: `CCSN-${level}`, prompted: res.kind === 'deny',
      protected: false, reason: res.kind === 'deny' ? `${res.ruleId}: ${res.reason}` : '',
    }) + '\n');
  }
}
