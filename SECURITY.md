# Security policy

Stagehand's remote surface is a WebSocket server inside the running game,
guarded by a per-session auth token. What it protects, and what it knowingly
does not, is written down in [docs/security.md](docs/security.md). Read that
first: some behavior (for example the browser-origin gap) is documented and
known.

## Reporting a vulnerability

Report it privately. Do not open a public issue.

Use GitHub's private vulnerability reporting:
<https://github.com/mrf/godot-stagehand/security/advisories/new>

Useful things to include: the version of the binary and the addon, the Godot
version, and the smallest sequence of WebSocket messages or tool calls that
shows the problem. In scope are the auth token, connection handling, the
unsafe-capability opt-in, file access from recording/replay, and anything that
lets an unauthenticated peer run a command.

This is a beta, pre-1.0 project maintained by one person. Expect an
acknowledgement, not a guaranteed turnaround.
