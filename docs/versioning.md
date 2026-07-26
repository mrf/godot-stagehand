# Versioning and protocol compatibility

Two independent numbers describe a Stagehand pair. Confusing them is what let
`plugin.cfg` say `0.2.0` while the binary and addon handshake said `0.1.0`.

| Number | Lives in | Meaning | Changes when |
|--------|----------|---------|--------------|
| **Release version** (`0.2.0`) | `internal/version/version.go` | Which build this is | Every release |
| **Protocol version** (`gwp/1`) | `internal/gwp/gwp.go` | Which wire contract it speaks | Only on a breaking GWP change |

A binary and an addon with **different release versions but the same protocol
version are supported** and connect normally. The protocol version is the
compatibility contract; the release version is not.

## One authoritative version source

`internal/version/version.go`'s `Version` constant is authoritative. Everything
else is a mirror:

| Mirror | Why it exists |
|--------|---------------|
| `addons/stagehand/plugin.cfg` (`version=`) | What the Godot editor's plugin list shows |
| `addons/stagehand/stagehand_version.gd` (`const VERSION`) | What the addon reports in the `ping` handshake |
| The `testdata/` and `examples/` addon copies | Byte-identical fixtures, kept in sync by `scripts/sync-addon-copies.sh` |
| The release tag `vX.Y.Z` | What the published artifacts are named after |

The Go constant is the root because it is the only one that must be *compiled
in*: `go build .` with no flags has to produce a binary reporting the real
version, and Godot cannot read Go link flags. Nothing is injected at build time
except commit metadata.

**Enforcement, not convention.** `go test ./internal/version/` walks every
`addons/stagehand/` copy in the repo — canonical, `testdata/`, `examples/` — and
fails if any mirror disagrees. `build-release.sh <tag>` re-checks the same set
plus the built binary's `--version` output, and the release workflow runs it in
`--verify-only` mode before publishing anything.

### Bumping the version

```bash
./scripts/set-version.sh X.Y.Z   # rewrites the constant and every mirror
go test ./internal/version/       # confirms they agree
git commit -am "release: X.Y.Z"
git tag vX.Y.Z
```

Do not hand-edit `plugin.cfg` or `stagehand_version.gd`; the script exists so a
bump cannot half-land. It edits only the canonical `addons/stagehand` copy and
then calls `scripts/sync-addon-copies.sh`, so the `testdata/` and `examples/`
fixtures stay byte-identical per [addon-sync-contract.md](addon-sync-contract.md).

## The handshake

`ping` is the negotiation. The addon answers with:

```json
{
  "status": "ok",
  "engine": "godot",
  "engine_version": "4.6.2-stable (official)",
  "stagehand_version": "0.2.0",
  "protocol_version": 1,
  "protocol": "gwp/1",
  "capabilities": ["core", "input", "screenshot", "wait", "performance", "recording"],
  "instance_token": "…"
}
```

Both `godot_launch` and `godot_connect` run `gwp.Negotiate` on that payload
before handing the session to a caller, so an incompatible pair is rejected at
connect time with an actionable message instead of failing opaquely on some
later tool call.

### Compatibility rules

1. `protocol_version` must equal `gwp.ProtocolVersion` exactly. A missing field
   means a pre-negotiation addon and is treated as version 0 — rejected.
2. Every capability in `gwp.RequiredCapabilities` must be advertised, or the
   session is rejected naming the missing ones.
3. Capabilities in `gwp.OptionalCapabilities` may be absent. The session
   connects and the connect result names what is unavailable.
4. A `stagehand_version` differing from the binary's is reported, not rejected.

Errors name both sides and the remedy: reinstall the addon
(`godot-stagehand setup --force <project>`) when the addon is older, upgrade the
binary when the addon is newer.

### Capabilities

| Capability | Required | Covers |
|------------|----------|--------|
| `core` | yes | `ping`, `get_tree`, `query_nodes`, `get_property`, `set_property`, `change_scene`, `get_game_state` |
| `input` | yes | `input_*` |
| `screenshot` | yes | `screenshot` |
| `wait` | yes | `wait_for_node`, `wait_for_property`, `wait_signal` |
| `performance` | no | `get_performance`, `assert_performance` |
| `recording` | no | `record_start`, `record_stop`, `replay` |
| `unsafe` | no | `evaluate`, `call_method` — advertised only under `STAGEHAND_ALLOW_UNSAFE=1` |

`unsafe` is dynamic on purpose: its absence tells a client up front that
`evaluate` and `call_method` will be refused, rather than surfacing as an error
on first use.

### Changing the protocol

Prefer adding a capability — additive changes need no version bump. Bump
`gwp.ProtocolVersion` **and** `PROTOCOL_VERSION`/`PROTOCOL_ID` in every
`stagehand_version.gd` only when an existing method's request or response shape
changes incompatibly. The version tests fail if the two sides disagree.

## `--version`

```
$ godot-stagehand --version
godot-stagehand 0.2.0
commit:    3f2a1c9…
built:     2026-07-24T18:22:11Z
go:        go1.25.0
protocol:  gwp/1
```

Commit and build time come from the Go toolchain's VCS stamps, or from
`-X …/internal/version.commit=…` when a build supplies them. `unknown` means the
binary was built outside a git checkout.
