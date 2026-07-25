extends RefCounted
## Version and Godot Wire Protocol (GWP) metadata for the Stagehand addon.
##
## This file is a MIRROR, not the source of truth. The authoritative version
## lives in the Go module at internal/version/version.go; scripts/set-version.sh
## rewrites every mirror (this file, plugin.cfg, and the copies under
## testdata/ and examples/) and `go test ./internal/version/` fails if any of
## them drift. Do not hand-edit the constants below — see docs/versioning.md.
##
## PROTOCOL_VERSION is the compatibility contract with the godot-stagehand
## binary: it must match the binary's gwp.ProtocolVersion exactly. VERSION may
## differ from the binary's build version — a version mix inside one protocol
## generation is supported. Bump PROTOCOL_VERSION only for a breaking wire
## change; prefer adding a capability for anything additive.

const VERSION: String = "0.2.0"
const PROTOCOL_VERSION: int = 1
const PROTOCOL_ID: String = "gwp/1"

## Capability vocabulary. Each name is a coarse family of GWP methods, and must
## stay in sync with the constants in internal/gwp/gwp.go.
const CAPABILITY_CORE: String = "core"
const CAPABILITY_INPUT: String = "input"
const CAPABILITY_SCREENSHOT: String = "screenshot"
const CAPABILITY_WAIT: String = "wait"
const CAPABILITY_PERFORMANCE: String = "performance"
const CAPABILITY_RECORDING: String = "recording"
## Advertised only when STAGEHAND_ALLOW_UNSAFE=1, so a client can tell up front
## whether `evaluate` and `call_method` will be served or rejected.
const CAPABILITY_UNSAFE: String = "unsafe"


## Returns the capability families this process can actually serve.
static func capabilities(allow_unsafe: bool) -> Array[String]:
	var advertised: Array[String] = [
		CAPABILITY_CORE,
		CAPABILITY_INPUT,
		CAPABILITY_SCREENSHOT,
		CAPABILITY_WAIT,
		CAPABILITY_PERFORMANCE,
		CAPABILITY_RECORDING,
	]
	if allow_unsafe:
		advertised.append(CAPABILITY_UNSAFE)
	return advertised
