# Godot version compatibility

Back to the [README](../README.md).

**Minimum supported version: Godot 4.3.** Development happens against 4.6.x
locally; 4.3-4.7 are all tested and supported.

| Godot version | Status | Notes |
|----------------|--------|-------|
| 4.2 | **Not supported** | Addon fails to parse; not exercised by CI (see below) |
| 4.3 | Supported (minimum) | |
| 4.4 | Supported | |
| 4.5 | Supported | |
| 4.6 | Supported (local dev baseline) | |
| 4.7 | Supported | |

Verified by running the full connect-and-drive protocol (parse → activate →
authenticated ping → `get_tree`/`find_nodes`/`click`/`screenshot`) against a
real headless Godot binary of each version. See `scripts/test-godot-compat.sh`
and the `gdscript-parse` job in `.github/workflows/ci.yml`, which runs this
matrix on every push/PR to `main`.

Some tools need a newer engine than the 4.3 floor: `godot_get_accessibility_tree`
and the `role:` selector require Godot 4.5+.

## Known incompatibilities

- **Godot 4.2: two 4.3+ features the addon depends on.** Verified against a
  real `Godot_v4.2-stable_linux.x86_64` binary:
  1. **GDScript `is not`.** The addon uses `is not` (e.g. `stagehand_server.gd`,
     `input_recorder.gd`, `protocol/json_rpc.gd`) for readability. That operator
     was added in [godotengine/godot#87939](https://github.com/godotengine/godot/pull/87939),
     first released in Godot 4.3, so 4.2 fails at load with
     `Parse Error: Expected type specifier after "is"`.
  2. **`OS.get_entropy()`** (`addons/stagehand/autoload/stagehand_server.gd`),
     which generates the per-session auth token. Also 4.3+; on 4.2 it reports
     `Static function "get_entropy()" not found in base "GDScriptNativeClass"`.
     It surfaces only once the `is not` errors are cleared.

  Supporting 4.2 would mean rewriting those checks as `not (x is T)` and adding
  a `Crypto.generate_random_bytes()` fallback in the token path. 4.2 is treated
  as unsupported rather than carrying both for one older release, and CI does
  not run it. Every job in the compat matrix is blocking, so a red job means a
  real regression.
