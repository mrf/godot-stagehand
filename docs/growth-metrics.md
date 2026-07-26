# Growth metrics — what we track

Operating companion to [growth-plan.md](growth-plan.md). That doc says *what to
do*; this one says *how we tell whether it worked*.

Stars are not on this list. A star costs one click from someone who never ran
the binary; it measures how good the README looked in a feed. Every metric below
costs the user real effort — downloading, installing, forking, writing up a
problem — so movement in one of them means somebody actually tried to use
stagehand. Ten users who file bugs beat 500 stars.

**Cadence: once a month.** Run the script, paste the row, glance at the trend.
Scheduling it is Mark's call — there is deliberately no cron and no CI job for
this, because a metric nobody reads is worse than no metric.

## How to take a reading

```bash
./scripts/growth-metrics.sh
```

Read-only `gh api` GETs only. It prints a detail block plus a ready-made table
row; paste the row into [Monthly readings](#monthly-readings) below and fill in
the one `FILL-IN` cell by hand. A metric whose API call fails prints
`UNAVAILABLE` rather than `0`, so a rate-limited run can't be mistaken for a
month of no adoption.

## Release-asset download counts, per platform

**Definition.** Cumulative downloads of each release binary, summed across every
published release: `godot-stagehand-linux-amd64`, `-darwin-amd64`,
`-darwin-arm64`, `-windows-amd64.exe`.

**Source.** `GET /repos/{owner}/{repo}/releases` → `assets[].download_count`.
Cumulative and monotonic, so the *delta* between two monthly rows is that
month's downloads. That delta is the number worth looking at; the running total
is just the bookkeeping that makes it computable.

**What good looks like.** A non-zero delta every month, and a platform mix that
isn't 100% one OS. The Setup wizard's "Download server binary" button resolves
`/releases/latest/download/<asset>`, so these counts are the closest proxy we
have to "someone completed the install flow."

**What bad looks like.** Delta near zero in a month where a release shipped, or
downloads concentrated entirely on Linux — that would suggest the reach is other
developers cloning, not game devs on Windows and macOS installing. Caveat worth
remembering: CI runners, mirrors, and scrapers also count, so treat small
absolute numbers as noise and watch the shape over several months.

## Godot Asset Library install count

**Definition.** The install/download number shown on the Asset Library listing.

**Source.** MANUAL — the script prints a labelled MANUAL line rather than
guessing. Read it off the listing page:
<https://godotengine.org/asset-library/asset?filter=stagehand>. If the
submission is still pending review, record `pending`; there is no number yet.

**What good looks like.** Any steady climb at all. The Asset Library is the one
discovery channel Godot developers actually browse, so it is the only metric
here that measures *reach* rather than *intent* — growth in it is what makes
growth in the others possible.

**What bad looks like.** Flat while GitHub download deltas are non-zero: the
listing isn't being found, and the fix is the listing (title, blurb, category,
screenshots), not the code.

## Issues opened by non-owner accounts

**Definition.** GitHub issues — not PRs, not bot activity — filed by any account
other than the repo owner, open and closed both.

**Source.** `GET /repos/{owner}/{repo}/issues?state=all`, dropping anything with
a `pull_request` key, any `user.type == "Bot"`, and the owner's own issues.

**What good looks like.** A trickle. This is the highest-value metric on the
page: filing an issue means someone installed stagehand, ran it against a real
project, hit a wall, and cared enough to write it up. One of these is worth more
signal than a hundred stars. Bug reports and confused questions count equally —
a question is a docs bug.

**What bad looks like.** Zero for months while downloads climb. That is the
worst pattern here, because it is ambiguous in a bad way: either people bounce
before they get anywhere, or they hit a wall and silently leave. If you see it,
go looking — the quickstart is the first suspect.

## Forks

**Definition.** Fork count, plus whether each fork has been pushed to.

**Source.** `GET /repos/{owner}/{repo}` → `forks_count`, and
`GET /repos/{owner}/{repo}/forks` for per-fork `created_at` / `pushed_at`.

**What good looks like.** Forks with pushes after their creation date — somebody
is modifying the code, which is a step past using it.

**What bad looks like.** Forks that were never pushed to. Those are bookmarks
with extra steps and belong in the same mental bucket as stars; the script
prints `pushed_at` precisely so you can tell the two apart.

## Monthly readings

One row per month. Download columns are cumulative totals, so read the trend as
the difference between consecutive rows. `AL installs` is the hand-filled Asset
Library number.

| Month | linux-amd64 | darwin-amd64 | darwin-arm64 | windows-amd64 | Total DL | AL installs | Non-owner issues | Forks |
| ----- | ----------- | ------------ | ------------ | ------------- | -------- | ----------- | ---------------- | ----- |
|       |             |              |              |               |          |             |                  |       |

## Notes

Leave a dated line here whenever a number needs context — a release that
shipped mid-month, a post that drove a spike, a fork worth watching. Without
it, a jump in the table is unattributable a year from now.
