# Contributing

Thanks for looking. This project is beta and maintained by one person, so small,
focused changes get reviewed fastest.

## Reporting bugs and requesting features

Open an issue and pick a form. The bug form asks for your Godot version, host OS
(say so if you are on WSL), godot-stagehand version, whether you drive it from an
MCP client, the CLI or the scenario runner, and the relevant log or trace.
Check [Troubleshooting](docs/troubleshooting.md) first.

Security problems go through [SECURITY.md](SECURITY.md), never a public issue.

## Building and testing

```bash
go build -o godot-stagehand .
go vet ./...
go test ./...        # no Godot needed
```

Tests that need a real Godot are behind build tags:

```bash
# Scenario runner against a headless Godot
GODOT_BIN=/path/to/godot go test -tags=godot -run '^TestScenarioRunner' .
# GDScript unit suite (GdUnit4, needs Godot 4.6+)
GODOT_BIN=/path/to/godot ./scripts/run-gdscript-tests.sh
```

## The bar for a change

- **Test first.** Write the failing test, watch it fail for the right reason,
  then make it pass. No skipped tests: guard with a build tag instead.
- **`go vet ./...` and `go test ./...` must be clean** before you open a PR.
- **GDScript must be strict-mode compliant.** The addon runs inside host projects
  that may turn every warning into an error: typed declarations, no discarded
  return values, no inferred types. Check it with
  `GODOT_BIN=<godot> ./scripts/ci-gdscript-warnings.sh check`.
- **Do not invent Godot APIs.** Check the [Godot docs](https://docs.godotengine.org)
  before calling something from GDScript.
- Validate selectors in Go with `selector.ParseChain()`, not in GDScript.

The full protocol is in [AGENTS.md](AGENTS.md); [docs/gdscript-testing.md](docs/gdscript-testing.md)
covers the addon test suite.

## Do not edit the vendored addon copies

`addons/stagehand` is the only authoritative copy of the addon. Two other trees
carry byte-identical checked-in copies:
`testdata/test_project/addons/stagehand` and
`examples/minimal-game/addons/stagehand`. Edit only `addons/stagehand`, then run

```bash
bash scripts/sync-addon-copies.sh
```

A drift test fails if the copies diverge. See
[docs/addon-sync-contract.md](docs/addon-sync-contract.md).

## Pull requests

Keep a PR to one change, say what it fixes, and include the test. The
maintainer tracks work in an internal queue, so a PR may be mirrored there; that
does not change anything about how you contribute.
