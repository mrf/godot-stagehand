# Issue triage: GitHub in, br as the working queue

GitHub Issues is the **inbound** channel only. The working queue stays in
`br` (beads_rust): the orchestrator and triage-spawn loops read it, and nothing
here moves the queue to GitHub. Do not mirror existing `br` issues outward.

## Path for an external report

1. An external reporter files a GitHub issue using the bug or feature form
   (`.github/ISSUE_TEMPLATE/`). Security reports go to `SECURITY.md` instead and
   never become public issues.
2. The maintainer reads it and, if it is actionable, mirrors it into `br` by
   hand. Workers do not file `br` tickets from GitHub, and do not touch GitHub
   (no `gh`, no remote operations).
3. The `br` ticket carries the GitHub issue number back with `--external-ref`
   (`br create --help` lists the flag):

   ```bash
   br create "title" --external-ref gh-123
   ```

   The ticket body should restate the repro rather than just link, so a worker
   can act without GitHub access.
4. After the `br` ticket merges, the maintainer closes the loop on GitHub
   (comment, close). That is a remote operation, so it is always the
   maintainer's.

## When to revisit a real migration

Only when a second person files an issue, a non-Dependabot PR arrives, or
contributors need to self-serve work. Until then the cost of dual bookkeeping
outweighs the benefit.
