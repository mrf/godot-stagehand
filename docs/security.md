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

`godot_launch` (and the CLI/scenario launcher) applies the same rule to the
game it starts. A non-loopback `host` is refused unless the launching process —
the MCP server, not the MCP caller — has `STAGEHAND_LAUNCH_ALLOW_REMOTE=1`. With
it, the game binds exactly the requested IP (never `0.0.0.0`), and the
`godot_launch` result carries a warning saying so. Hostnames other than
`localhost` are refused outright, since there is no address to bind them to.

**Connection limits.** A peer that completes the WebSocket upgrade must
authenticate within 5 seconds or it is closed (code 1008). A failed
`authenticate` is answered and then closes the connection, so each socket gets
one guess. At most 32 peers are held at once. A configured
`STAGEHAND_AUTH_TOKEN` shorter than 32 characters draws a startup warning;
prefer the generated 64-hex-character token.

**Browser origins (known gap).** Godot 4.6's `WebSocketPeer` gives the server
no access to the upgrade request's headers, and `StreamPeer` cannot peek at the
request without consuming it, so Stagehand cannot check or reject a browser
`Origin`. Any web page in a local browser can therefore open sockets to the
port. Without the token it cannot run commands, and each socket is closed
within 5 seconds, but a page that keeps reopening sockets can still crowd out a
real client and delay the quit-on-disconnect timer.

Expression evaluation and arbitrary method calls are disabled unless the
session separately opts into unsafe capabilities. Authentication limits who can
reach automation; unsafe opt-in controls what an authenticated peer may execute.

**Self-protection.** The session token and the unsafe opt-in live on the
`StagehandServer` node, so the remote surface is kept off it: `get_property`,
`set_property`, `wait_for_property`, `call_method`, and `query_nodes` with
`properties` refuse (`not_supported`) any selector that resolves to
`/root/StagehandServer` or any node under it, and `get_tree` lists those nodes
without their property values. Separately, `get_property` and `set_property`
refuse private (`_`-prefixed) members and `script` on every node, and so does
`wait_for_property`, which reads only a property, a `.`/`:` sub-path, or
metadata — never a `get_<name>()` method, which would run game code in safe
mode. Unsafe mode can only be enabled by relaunching with
`STAGEHAND_ALLOW_UNSAFE=1`.

**The `call_method` blocklist is a safety net, not a sandbox.** `call_method`
already requires unsafe mode; the blocklist (`free`, `queue_free`, `set_script`,
tree surgery, `call`/`callv`/`set` and the other dynamic-dispatch methods, plus
any `_`-prefixed name) only guards against an automation mistake. Any game
method can still be destructive.

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

## Scenario files are code

Running `godot-stagehand run <scenario.json>` can execute arbitrary programs: a
launch-mode scenario's `target.godot_bin` is run as the Godot binary, and
`target.allow_unsafe` turns on expression evaluation and arbitrary method calls
in the launched game. A pull request that adds `"godot_bin": "./evil.sh"` runs
that script in any CI job that runs the scenario. Review scenario changes as
you would a script, and never run a scenario from an untrusted source. The same
applies to an MCP caller of `godot_launch`, which can pass `godot_bin` and
`allow_unsafe` too.

What the runner does confine is where a scenario writes: `baseline_dir` must be
a relative path inside the scenario file's directory, and screenshot outputs and
baseline names cannot escape the run's directories (see [CLI](cli.md)). The
parse-time check is lexical, so `save_baseline` also resolves symlinks before
writing and refuses a baseline directory or file that resolves outside the
scenario directory. Baselines outside the scenario directory need the
operator's `--baseline-dir` flag, which is trusted and may point through
symlinks.
