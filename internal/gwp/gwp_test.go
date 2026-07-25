package gwp_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwp/gwptest"
	"github.com/mrf/godot-stagehand/internal/version"
)

func TestNegotiateAcceptsCompatibleHandshake(t *testing.T) {
	info, err := gwp.Negotiate(gwptest.Handshake(nil))
	if err != nil {
		t.Fatalf("Negotiate() error = %v, want nil", err)
	}
	if info.ProtocolVersion != gwp.ProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", info.ProtocolVersion, gwp.ProtocolVersion)
	}
	if info.StagehandVersion != version.Version {
		t.Errorf("StagehandVersion = %q, want %q", info.StagehandVersion, version.Version)
	}
	if info.EngineVersion != "4.4.1.stable" {
		t.Errorf("EngineVersion = %q, want %q", info.EngineVersion, "4.4.1.stable")
	}
	if info.InstanceToken != "abc123" {
		t.Errorf("InstanceToken = %q, want %q", info.InstanceToken, "abc123")
	}
	if len(info.MissingOptional) != 0 {
		t.Errorf("MissingOptional = %v, want empty", info.MissingOptional)
	}
	if info.VersionSkew {
		t.Error("VersionSkew = true, want false for a matching addon build")
	}
	if !info.Has(gwp.CapabilityScreenshot) {
		t.Errorf("Has(%q) = false, want true", gwp.CapabilityScreenshot)
	}
}

func TestNegotiateRejectsNewerProtocol(t *testing.T) {
	_, err := gwp.Negotiate(gwptest.Handshake(map[string]any{"protocol_version": gwp.ProtocolVersion + 1}))
	if err == nil {
		t.Fatal("Negotiate() error = nil, want an incompatibility error")
	}
	var incompatible *gwp.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("Negotiate() error = %T, want *gwp.IncompatibleError", err)
	}
	if incompatible.PeerProtocolVersion != gwp.ProtocolVersion+1 {
		t.Errorf("PeerProtocolVersion = %d, want %d", incompatible.PeerProtocolVersion, gwp.ProtocolVersion+1)
	}
	// The remedy must be actionable and must point at the *binary*, since the
	// addon is the newer side.
	if !strings.Contains(err.Error(), "godot-stagehand binary") {
		t.Errorf("error %q does not tell the user to upgrade the binary", err)
	}
}

func TestNegotiateRejectsOlderProtocol(t *testing.T) {
	_, err := gwp.Negotiate(gwptest.Handshake(map[string]any{"protocol_version": gwp.ProtocolVersion - 1}))
	if err == nil {
		t.Fatal("Negotiate() error = nil, want an incompatibility error")
	}
	if !strings.Contains(err.Error(), "setup --force") {
		t.Errorf("error %q does not tell the user how to reinstall the addon", err)
	}
}

// A pre-negotiation addon reports no protocol_version at all. It must be
// rejected with the same reinstall remedy rather than silently accepted.
func TestNegotiateRejectsLegacyAddonWithoutProtocolVersion(t *testing.T) {
	_, err := gwp.Negotiate(gwptest.Handshake(map[string]any{
		"protocol_version": nil,
		"protocol":         nil,
		"capabilities":     nil,
	}))
	if err == nil {
		t.Fatal("Negotiate() error = nil, want an incompatibility error")
	}
	var incompatible *gwp.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("Negotiate() error = %T, want *gwp.IncompatibleError", err)
	}
	if incompatible.PeerProtocolVersion != 0 {
		t.Errorf("PeerProtocolVersion = %d, want 0 for a pre-negotiation addon", incompatible.PeerProtocolVersion)
	}
	if !strings.Contains(err.Error(), "setup --force") {
		t.Errorf("error %q does not tell the user how to reinstall the addon", err)
	}
}

// Same protocol major, fewer optional capabilities: the session is usable but
// degraded, so it connects and reports what is missing.
func TestNegotiateAcceptsAddonMissingOptionalCapabilities(t *testing.T) {
	info, err := gwp.Negotiate(gwptest.Handshake(map[string]any{
		"capabilities": gwp.RequiredCapabilities,
	}))
	if err != nil {
		t.Fatalf("Negotiate() error = %v, want nil for a required-only capability set", err)
	}
	if len(info.MissingOptional) != len(gwp.OptionalCapabilities) {
		t.Errorf("MissingOptional = %v, want all optional capabilities %v",
			info.MissingOptional, gwp.OptionalCapabilities)
	}
	if info.Has(gwp.CapabilityRecording) {
		t.Errorf("Has(%q) = true, want false", gwp.CapabilityRecording)
	}
	summary := info.Summary()
	if !strings.Contains(summary, "Unavailable capabilities:") || !strings.Contains(summary, gwp.CapabilityRecording) {
		t.Errorf("Summary() = %q, want it to name the unavailable capabilities", summary)
	}
}

// Same protocol major, missing a *required* capability: unusable, reject.
func TestNegotiateRejectsAddonMissingRequiredCapability(t *testing.T) {
	reduced := []string{}
	for _, capability := range gwp.RequiredCapabilities {
		if capability != gwp.CapabilityScreenshot {
			reduced = append(reduced, capability)
		}
	}
	_, err := gwp.Negotiate(gwptest.Handshake(map[string]any{"capabilities": reduced}))
	if err == nil {
		t.Fatal("Negotiate() error = nil, want an incompatibility error")
	}
	if !strings.Contains(err.Error(), gwp.CapabilityScreenshot) {
		t.Errorf("error %q does not name the missing capability", err)
	}
	if !strings.Contains(err.Error(), "setup --force") {
		t.Errorf("error %q does not tell the user how to reinstall the addon", err)
	}
}

// A build-version mismatch inside the same protocol major is allowed: the
// protocol is the compatibility contract, not the marketing version.
func TestNegotiateAcceptsMixedBuildVersions(t *testing.T) {
	info, err := gwp.Negotiate(gwptest.Handshake(map[string]any{"stagehand_version": "0.0.1"}))
	if err != nil {
		t.Fatalf("Negotiate() error = %v, want nil for a same-protocol version skew", err)
	}
	if !info.VersionSkew {
		t.Error("VersionSkew = false, want true when the addon build differs")
	}
	if summary := info.Summary(); !strings.Contains(summary, "0.0.1") {
		t.Errorf("Summary() = %q, want it to name the addon version", summary)
	}
}

func TestNegotiateRejectsNonGodotPeer(t *testing.T) {
	if _, err := gwp.Negotiate(gwptest.Handshake(map[string]any{"engine": "unity"})); err == nil {
		t.Error("Negotiate() error = nil, want an error for a non-Godot peer")
	}
	if _, err := gwp.Negotiate(gwptest.Handshake(map[string]any{"status": "degraded"})); err == nil {
		t.Error("Negotiate() error = nil, want an error for a non-ok status")
	}
}

func TestNegotiateRejectsMalformedPayload(t *testing.T) {
	if _, err := gwp.Negotiate(json.RawMessage(`{"status":`)); err == nil {
		t.Error("Negotiate() error = nil, want a parse error")
	}
}
