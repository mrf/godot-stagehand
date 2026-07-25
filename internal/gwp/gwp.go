// Package gwp holds the Godot Wire Protocol compatibility contract: the
// protocol version this build speaks, the capability vocabulary, and the
// handshake negotiation both the launch and connect paths run before a session
// is handed to the caller.
//
// The compatibility rule is deliberately blunt: ProtocolVersion is a single
// integer that must match exactly. Build versions (internal/version) may differ
// freely inside one protocol generation — the protocol is the contract, the
// release version is not. Bump ProtocolVersion only for a breaking wire change,
// and add a capability instead whenever the change is additive.
package gwp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mrf/godot-stagehand/internal/version"
)

// ProtocolVersion is the GWP generation this build speaks. Peers must report
// the same integer or the session is refused.
const ProtocolVersion = 1

// ProtocolID is the human-readable protocol identifier reported in handshakes.
const ProtocolID = "gwp/1"

// Capability names. A capability is a coarse family of GWP methods; the addon
// advertises the families it can actually serve in the current process.
const (
	// CapabilityCore covers ping, tree, query, property, and scene methods.
	CapabilityCore = "core"
	// CapabilityInput covers the input_* simulation methods.
	CapabilityInput = "input"
	// CapabilityScreenshot covers viewport capture.
	CapabilityScreenshot = "screenshot"
	// CapabilityWait covers the wait_* polling methods.
	CapabilityWait = "wait"
	// CapabilityPerformance covers get_performance and assert_performance.
	CapabilityPerformance = "performance"
	// CapabilityRecording covers record_start, record_stop, and replay.
	CapabilityRecording = "recording"
	// CapabilityUnsafe covers evaluate and call_method. The addon advertises it
	// only when launched with STAGEHAND_ALLOW_UNSAFE=1, so its absence is a
	// deliberate opt-out rather than an old build.
	CapabilityUnsafe = "unsafe"
)

// RequiredCapabilities must all be present or the session is refused: every
// non-optional MCP tool surface depends on them.
var RequiredCapabilities = []string{
	CapabilityCore,
	CapabilityInput,
	CapabilityScreenshot,
	CapabilityWait,
}

// OptionalCapabilities degrade gracefully — their absence disables the matching
// tools but leaves the session usable.
var OptionalCapabilities = []string{
	CapabilityPerformance,
	CapabilityRecording,
	CapabilityUnsafe,
}

// KnownCapability reports whether name is part of the capability vocabulary.
func KnownCapability(name string) bool {
	return slices.Contains(RequiredCapabilities, name) || slices.Contains(OptionalCapabilities, name)
}

// reinstallRemedy is the one actionable instruction for every "the addon is the
// wrong generation" failure.
const reinstallRemedy = "reinstall the bundled addon with `godot-stagehand setup --force <project>` and restart the game"

// Info is the negotiated view of a peer, returned once the handshake is
// accepted.
type Info struct {
	// EngineVersion is the Godot engine version string.
	EngineVersion string
	// StagehandVersion is the addon's build version.
	StagehandVersion string
	// ProtocolVersion is the negotiated GWP generation.
	ProtocolVersion int
	// Capabilities is everything the peer advertised, in its order.
	Capabilities []string
	// MissingOptional lists optional capabilities the peer does not serve.
	MissingOptional []string
	// VersionSkew reports that the addon build differs from this binary's.
	VersionSkew bool
	// InstanceToken echoes the per-launch token used to prove process identity.
	InstanceToken string
}

// Has reports whether the peer advertised the named capability.
func (i *Info) Has(capability string) bool {
	return slices.Contains(i.Capabilities, capability)
}

// Summary is a one-line, human-readable description of the negotiated session,
// suitable for appending to a tool result.
func (i *Info) Summary() string {
	summary := fmt.Sprintf("Godot %s, stagehand addon %s, protocol %s",
		i.EngineVersion, i.StagehandVersion, ProtocolID)
	if i.VersionSkew {
		summary += fmt.Sprintf(" (this binary is %s; same protocol generation, so the mix is supported)", version.Version)
	}
	if len(i.MissingOptional) > 0 {
		summary += fmt.Sprintf(". Unavailable capabilities: %s", strings.Join(i.MissingOptional, ", "))
	}
	return summary
}

// IncompatibleError reports a peer this build cannot drive. Its message always
// names both sides and the remedy.
type IncompatibleError struct {
	// PeerProtocolVersion is the peer's GWP generation; 0 means it predates
	// protocol negotiation entirely.
	PeerProtocolVersion int
	// PeerStagehandVersion is the addon build version, when it reported one.
	PeerStagehandVersion string
	// MissingRequired lists required capabilities the peer did not advertise.
	MissingRequired []string
	// Reason is the rendered, actionable explanation.
	Reason string
}

func (e *IncompatibleError) Error() string { return e.Reason }

// handshake mirrors the ping result. ProtocolVersion is a pointer so a legacy
// addon that omits the field is distinguishable from one reporting 0.
type handshake struct {
	Status           string   `json:"status"`
	Engine           string   `json:"engine"`
	EngineVersion    string   `json:"engine_version"`
	StagehandVersion string   `json:"stagehand_version"`
	ProtocolVersion  *int     `json:"protocol_version"`
	Protocol         string   `json:"protocol"`
	Capabilities     []string `json:"capabilities"`
	InstanceToken    string   `json:"instance_token"`
}

// Negotiate parses a ping result and either returns the negotiated session
// info or an error explaining precisely why the pair cannot talk.
func Negotiate(raw json.RawMessage) (*Info, error) {
	var peer handshake
	if err := json.Unmarshal(raw, &peer); err != nil {
		return nil, fmt.Errorf("malformed Stagehand handshake: %w", err)
	}
	if peer.Status != "ok" || peer.Engine != "godot" {
		return nil, fmt.Errorf("unexpected Stagehand handshake: status=%q, engine=%q", peer.Status, peer.Engine)
	}

	peerProtocol := 0
	if peer.ProtocolVersion != nil {
		peerProtocol = *peer.ProtocolVersion
	}
	if peerProtocol != ProtocolVersion {
		return nil, &IncompatibleError{
			PeerProtocolVersion:  peerProtocol,
			PeerStagehandVersion: peer.StagehandVersion,
			Reason:               protocolMismatchReason(peerProtocol, peer.StagehandVersion),
		}
	}

	var missingRequired []string
	for _, capability := range RequiredCapabilities {
		if !slices.Contains(peer.Capabilities, capability) {
			missingRequired = append(missingRequired, capability)
		}
	}
	if len(missingRequired) > 0 {
		return nil, &IncompatibleError{
			PeerProtocolVersion:  peerProtocol,
			PeerStagehandVersion: peer.StagehandVersion,
			MissingRequired:      missingRequired,
			Reason: fmt.Sprintf(
				"Stagehand addon %s speaks protocol %s but does not provide the required capabilities: %s. "+
					"This build requires %s. To fix: %s.",
				describeAddonVersion(peer.StagehandVersion),
				ProtocolID,
				strings.Join(missingRequired, ", "),
				strings.Join(RequiredCapabilities, ", "),
				reinstallRemedy,
			),
		}
	}

	var missingOptional []string
	for _, capability := range OptionalCapabilities {
		if !slices.Contains(peer.Capabilities, capability) {
			missingOptional = append(missingOptional, capability)
		}
	}

	return &Info{
		EngineVersion:    peer.EngineVersion,
		StagehandVersion: peer.StagehandVersion,
		ProtocolVersion:  peerProtocol,
		Capabilities:     peer.Capabilities,
		MissingOptional:  missingOptional,
		VersionSkew:      peer.StagehandVersion != version.Version,
		InstanceToken:    peer.InstanceToken,
	}, nil
}

// protocolMismatchReason names the older side, because that is the side the
// user has to move.
func protocolMismatchReason(peerProtocol int, peerVersion string) string {
	switch {
	case peerProtocol == 0:
		return fmt.Sprintf(
			"Stagehand addon %s predates protocol negotiation (it reports no protocol_version) "+
				"and cannot be driven by this godot-stagehand %s binary, which speaks %s. To fix: %s.",
			describeAddonVersion(peerVersion), version.Version, ProtocolID, reinstallRemedy,
		)
	case peerProtocol > ProtocolVersion:
		return fmt.Sprintf(
			"Stagehand addon %s speaks protocol gwp/%d but this godot-stagehand %s binary speaks %s. "+
				"The addon is newer: upgrade the godot-stagehand binary to a release matching the addon "+
				"(https://github.com/mrf/godot-stagehand/releases/latest).",
			describeAddonVersion(peerVersion), peerProtocol, version.Version, ProtocolID,
		)
	default:
		return fmt.Sprintf(
			"Stagehand addon %s speaks protocol gwp/%d but this godot-stagehand %s binary speaks %s. "+
				"The addon is older. To fix: %s.",
			describeAddonVersion(peerVersion), peerProtocol, version.Version, ProtocolID, reinstallRemedy,
		)
	}
}

func describeAddonVersion(peerVersion string) string {
	if peerVersion == "" {
		return "(unknown version)"
	}
	return peerVersion
}
