// Fails unless the mod's mods API calls are all on the allowlist in
// docs/design/claude-code-plugin.md (D7, "Smallest call surface"), so the
// `calls:` line a cautious user reads in `claude plugin validate` stays small.
//
// Reads `claude plugin validate --json <plugin>` on stdin. The CLI reports a
// hooks module's calls only as a note such as
//   ./register.ts calls: $.command.register, $.mcp.call (via callFromPane)
// so this parses that line, and fails when it is missing rather than passing
// on a report it cannot read.
import { readFileSync } from 'node:fs'

const ALLOWED = new Set(['command.register', 'ui.open', 'ui.resolve', 'ui.invalidate', 'mcp.call'])
// D7 keeps the mod free of environment variables and $.state.
const FORBIDDEN_NOTES = / (env reads|env writes|state reads|state writes): /

const report = JSON.parse(readFileSync(0, 'utf8'))
const notes = (report.contents ?? []).flatMap((entry) => entry.notes ?? [])
const failures = []

const callLines = notes.filter((note) => / calls: /.test(note))
if (callLines.length === 0) {
  failures.push('no "calls:" note in the validate report; did its format change?')
}
for (const line of callLines) {
  for (const [, call] of line.matchAll(/\$\.([A-Za-z]+\.[A-Za-z]+)/g)) {
    if (!ALLOWED.has(call)) failures.push(`$.${call} is not on the allowlist (${line})`)
  }
}
for (const note of notes.filter((n) => FORBIDDEN_NOTES.test(n))) {
  failures.push(`the mod must not read or write env or state: ${note}`)
}

if (failures.length > 0) {
  for (const failure of failures) console.error(`calls allowlist: ${failure}`)
  process.exit(1)
}
console.log(`calls allowlist: ok (${callLines.join('; ')})`)
