# Godot Stagehand

[![CI](https://github.com/mrf/godot-stagehand/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mrf/godot-stagehand/actions/workflows/ci.yml)
[![GitHub issues](https://img.shields.io/github/issues/mrf/godot-stagehand)](https://github.com/mrf/godot-stagehand/issues)
[![GitHub pull requests](https://img.shields.io/github/issues-pr/mrf/godot-stagehand)](https://github.com/mrf/godot-stagehand/pulls)
[![License](https://img.shields.io/github/license/mrf/godot-stagehand)](LICENSE)

Playwright, but for game engines.

**Drive your *running* Godot game from outside the engine.** Click real buttons,
read real node state, capture real frames and diff them against saved
baselines.

One Go binary. Two frontends over the same live connection:
- **MCP server.** Claude or any other agent plays your game and finds the bugs
  you would have found by clicking.
- **CLI with a scenario runner.** The same checks run in CI. JUnit output,
  stable exit codes, no MCP client anywhere.

**See it work:** [`examples/minimal-game`](examples/minimal-game#watch-an-agent-play-it-one-command)
is one command. Clone it and watch an agent click a button in a real running
Godot scene, then assert the result.

**Status: beta, pre-1.0.** Prebuilt binaries for Linux, macOS (Intel and Apple
Silicon) and Windows. Tool schemas and the wire protocol may still change
between minor versions.

## Install

```bash
# 1. Get the binary (Linux shown; macOS and Windows builds are on the same page)
curl -fsSLo godot-stagehand \
  https://github.com/mrf/godot-stagehand/releases/latest/download/godot-stagehand-linux-amd64
chmod +x godot-stagehand

# 2. Install the addon into your Godot project (idempotent)
./godot-stagehand setup /path/to/your/godot/project

# 3. Run your game with Stagehand on
godot --path /path/to/your/project --stagehand
```

`setup` prints the MCP client config snippet and the command to run your game.
Your game then prints a one-session auth token. Keep it private, and give it to
`godot_connect`.

Rather not use a terminal? Enable the addon in **Project → Project Settings →
Plugins**, then click **Setup…** in the editor toolbar. That wizard downloads the
binary, writes the config, and tests the connection. Either path is walked
through step by step in the **[Quickstart](docs/quickstart.md)**.

## Use it

From an MCP client (Claude Code, Claude Desktop, Cursor, anything speaking MCP):

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/absolute/path/to/godot-stagehand"
    }
  }
}
```

From a terminal or a CI job:

```bash
export STAGEHAND_AUTH_TOKEN=<the token this Godot session printed>
godot-stagehand find --port 26788 'class:Button' --properties text
godot-stagehand run scenarios/menu-smoke.json --out-dir ci-artifacts
```

`run` executes a declarative list of launch, action, wait and assertion steps
against a real Godot build and exits nonzero on failure. Exit `5` means a real
regression. `--out-dir` collects `report.json`, `junit.xml`, `rpc-trace.json`,
`godot.log`, screenshots and diff images.

Stagehand binds to `127.0.0.1` and rejects every command until the peer supplies
the session token, but it is a dev control plane, not a hardened endpoint.
**Read the [security boundary](docs/security.md) before you expose anything.**

## Docs

| | |
|---|---|
| [Quickstart](docs/quickstart.md) | Install and first command, step by step |
| [Tool reference](docs/tools.md) | Every MCP tool, and what to build with them |
| [CLI and scenario runner](docs/cli.md) | Commands, scenario format, exit codes, CI recipes |
| [Selectors](docs/selectors.md) | Targeting nodes by path, name, class, group, text, role |
| [Configuration](docs/configuration.md) | Flags, env vars, timeouts, running several agents at once |
| [Security boundary](docs/security.md) | Auth, remote binding, unsafe methods |
| [Architecture](docs/architecture.md) | How the addon, the binary and your client fit together |
| [Compatibility](docs/compatibility.md) | Godot 4.3 to 4.7, and why not 4.2 |
| [Troubleshooting](docs/troubleshooting.md) | When it won't connect, or the screenshots are black |
| [Comparison](docs/comparison.md) | Versus editor-automation tools and in-engine test frameworks |
| [Visual regression](docs/visual-regression.md) | Baselines, diffing, and the [CI gate contract](docs/visual-smoke-contract.md) |
| [Agent skill](skills/stagehand.md) | Drop-in skill file that teaches an agent the whole workflow |
| [Windows / WSL](docs/windows-setup.md) | Bridging Godot on Windows with a client in WSL |

## Development

```bash
go vet ./...          # lint
go test ./...         # Go tests (no Godot needed)
# Scenario runner against a real headless Godot
GODOT_BIN=/path/to/godot go test -tags=godot -run '^TestScenarioRunner' .
# GDScript unit suite (GdUnit4, headless, needs Godot 4.6+)
GODOT_BIN=/path/to/godot ./scripts/run-gdscript-tests.sh
```

Read the [GDScript testing guide](docs/gdscript-testing.md) for the suite layout
and strict-mode rules, the [addon sync contract](docs/addon-sync-contract.md)
before editing any copy of the addon, and the
[error model](docs/error-model.md) before adding a handler that can fail.

## License

MIT. See [LICENSE](LICENSE).
