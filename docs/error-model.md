# Error model

How a failure travels from a GDScript handler inside the game, across the Godot
Wire Protocol, to the agent reading an MCP tool result or the shell reading a
CLI exit code.

**The rule: a failed call never looks like a successful one.** Every layer has a
distinct failure channel, and each layer maps the one below it onto its own.

## 1. GDScript — the canonical failure envelope

`addons/stagehand/core/errors.gd` (`StagehandErrors`) defines the one shape a
handler returns when it cannot fulfil a well-formed request:

```gdscript
{
    "error": "Property not found: hitpoints",   # human-readable
    "error_code": "property_not_found",         # stable machine kind
    "details": {                                # optional structured context
        "selector": "name:Player",
        "node_path": "/root/Game/Player",
        "node_class": "CharacterBody2D",
        "next_action": "Call get_tree with the properties argument ...",
    },
}
```

Build it with `StagehandErrors.make(code, message, details)`, or one of the
shorthands for the two most common cases — `node_not_found(selector)` and
`missing_param(name)`. Never hand-roll `{"error": "..."}`: a bare string map
carries no kind and no remediation, and both are load-bearing downstream.

`details.next_action` is not decoration. It is what an agent reads to decide its
next tool call, so every failure that has an obvious remedy should state it.

### The kinds

| `error_code` | Meaning |
| --- | --- |
| `invalid_params` | A parameter was missing, wrongly typed, or inconsistent |
| `invalid_selector` | A selector string was unusable |
| `node_not_found` | A selector parsed but matched nothing live |
| `property_not_found` | The node exists; the property does not |
| `method_not_found` | The node exists; the method does not |
| `not_supported` | The node exists but cannot do this |
| `invalid_value` | A value could not be converted or did not take effect |
| `scene_not_found` / `scene_change_failed` | Scene missing / engine refused the switch |
| `evaluation_failed` | An expression failed to parse or raised |
| `timeout` | A wait or poll gave up |
| `renderer_unavailable` | The renderer produced no pixels (headless / no GPU) |
| `recorder_state` | The recorder was asked to do what its state forbids |
| `io_error` | A file could not be opened, read, written, or parsed |
| `internal` | An aborted handler, or a condition with no better kind |

The vocabulary is deliberately coarse. The precise condition lives in the
message and `details`; the kind exists so a client can branch without parsing
prose. Add a kind only when a client would genuinely act differently on it —
`renderer_unavailable` is separate from `timeout` for exactly that reason: a
visual check skips on a GPU-less session but fails on a real capture timeout.

## 2. Dispatch — envelope to JSON-RPC error

`StagehandCommandRouter.dispatch_checked` awaits the handler and classifies what
came back into `{"outcome": "ok", "result": ...}` or
`{"outcome": "error", "error": <envelope>}`. Two things collapse into the error
outcome:

1. The handler returned a canonical envelope.
2. The handler **aborted** on an unhandled GDScript runtime error. GDScript has
   no try/catch — confirmed by instrumented reproduction against Godot 4.6.2, an
   abort does not unwind; it aborts only the erroring function and resumes the
   awaiter with that function's declared-type default value. Every handler
   declares `-> Dictionary` and always returns a non-empty Dictionary on both
   its success and failure paths, so an exactly-empty result is an unambiguous
   abort signal. It becomes an `internal` envelope rather than a bogus success
   (`docs/audits/2026-07-08-implementation-audit.md` finding S8).

`StagehandServer._dispatch_and_respond` then sends a JSON-RPC 2.0 **error
response** — never a `result`:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "error": {
    "code": -32004,
    "message": "Property not found: hitpoints",
    "data": {
      "error_code": "property_not_found",
      "method": "get_property",
      "selector": "name:Player",
      "details": { "node_class": "CharacterBody2D", "next_action": "..." }
    }
  }
}
```

`selector` is echoed from the request parameters, so a client can attribute a
failure to a target without re-parsing what it sent.

### Code mapping

`StagehandErrors.json_rpc_code` is the authority; `internal/godotconn/protocol.go`
mirrors it.

| JSON-RPC code | Kinds | Meaning |
| --- | --- | --- |
| `-32602` | `invalid_params`, `invalid_selector`, `invalid_value` | The request was malformed |
| `-32603` | `internal` | The addon broke |
| `-32004` | `node_not_found`, `property_not_found`, `method_not_found`, `scene_not_found` | Well-formed request, absent target |
| `-32005` | `timeout` | Gave up before the condition held |
| `-32006` | everything else | Any other handler failure |

Protocol-level faults keep their reserved codes and are raised before dispatch:
`-32700` parse error, `-32600` invalid request, `-32601` unknown method, plus
the session codes `-32001` auth required, `-32002` auth failed, `-32003` unsafe
capability required.

An unrecognised kind maps to `-32006`, never to a reserved code — a future
addon-side kind must not masquerade as a protocol fault.

## 3. Go — JSON-RPC error to frontend failure

`godotconn.Call` returns the `*RPCError` as a Go `error`. `RPCError.Failure(method)`
decodes `error.data` into a `gwp.RemoteFailure`, and `Describe()` renders the one
line every frontend shows:

```
get_property failed: Property not found: hitpoints (code=property_not_found, selector="name:Player") — Call get_tree with the properties argument ... [node_class=CharacterBody2D]
```

Method, then message, then the machine kind and selector, then the remediation
hint, then any remaining details sorted by key. Deterministic, so tests and
trace reports can assert on it.

- **MCP** (`internal/mcpserver`) turns it into an `isError` tool result.
- **CLI / scenario runner** (`internal/gwpop`) classifies it into a `Kind` that
  drives the exit code: `-32602`/`-32600`/`-32601` are `KindUsage` (the caller's
  mistake), `-32005` is `KindTimeout`, everything else is `KindRemote`. A
  JSON-RPC error is a *reply*, so it is never `KindTransport`.

Failures caught in Go before the request goes out — an unparseable selector, no
connected instance, an exhausted in-flight call budget — become `isError`
results directly. A Go-side deadline (`context.DeadlineExceeded`) is reported as
a timeout naming the method and pointing at `godot_status`, because "Godot never
answered" is a different diagnosis from "the handler ran and failed".

## Backward compatibility

Addons vendored into host projects before this model reported failures as an
`{"error", "error_code", "details"}` triple inside an *otherwise successful*
result. `mcpserver.checkGodotResult` and `gwpop.checkAddonError` still inspect
every result for that shape and surface it as a failure, so upgrading the Go
binary against an old addon does not silently turn old failures into successes.
Both paths are dead against a current addon.

## Adding a handler

1. Return `StagehandErrors.make(...)` for every failure — including the ones
   that "can't happen".
2. Pick the closest existing kind. Only add one if a client would branch on it.
3. Put the target (`selector`, `node_path`, `property`, ...) in `details`, and a
   `next_action` whenever there is a sensible remedy.
4. Never return an empty Dictionary on a success path — that is the abort signal
   (see §2).
5. Add a GdUnit case asserting the kind, not just the message text.
