# Security boundary

Read this before you enable Stagehand on anything you care about. Back to the
[README](../README.md).

Stagehand is a development automation control plane, not a public game
endpoint. It binds to `127.0.0.1` by default and rejects every command on each
WebSocket peer until that peer supplies the current session token. `godot_launch`
creates and authenticates with a fresh secret automatically; manual/editor
starts generate one and print it in the local Godot output.

Remote binding requires both a non-loopback `STAGEHAND_BIND_ADDRESS` and
`STAGEHAND_ALLOW_REMOTE=1`, and emits a prominent warning. Use it only on a
trusted network with an appropriate host firewall, and never publish the token.
The WebSocket transport is not encrypted; this boundary is not a substitute for
TLS, network isolation, or a trustworthy local host.
Expression evaluation and arbitrary method calls are disabled unless the
session separately opts into unsafe capabilities. Authentication limits who can
reach automation; unsafe opt-in controls what an authenticated peer may execute.

Input recording is confined the same way. `record_start` writes, and `replay`
reads, only files under `user://stagehand_recordings/`: a path is either
relative to that directory (`run1.json`, `runs/run1.json`) or spelled out in
full (`user://stagehand_recordings/run1.json`). Segments may use letters,
digits, `_`, `-` and `.`; `res://`, any other scheme or bare `user://` path,
absolute paths, drive letters, backslashes, and `.`/`..` segments are refused
with `invalid_params` before any file is opened — so a caller can neither
truncate a project script or `~/.bashrc` nor use replay to probe whether a file
exists. The addon (`resolve_recording_path` in `input_recorder.gd`) and the Go
clients (`gwpop.ResolveRecordingPath`, used by the MCP tools, CLI and scenario
runner) apply the identical rule. `STAGEHAND_ALLOW_UNSAFE=1` lifts it in the
addon only; the Go clients always apply it.

**Read this before running setup.** Once a game is running with
Stagehand enabled, anyone who has the session token can inspect and mutate its
state, including calling arbitrary methods if unsafe capabilities are opted
in. Treat it like any other local dev/debug port.

Release exports ignore the ordinary CLI flag and `STAGEHAND_ENABLED` unless
`STAGEHAND_ALLOW_RELEASE=1` is also set deliberately — see
[Configuration](configuration.md).
