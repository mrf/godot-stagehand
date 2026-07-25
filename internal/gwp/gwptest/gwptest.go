// Package gwptest provides the handshake fixture shared by every test that
// stands in for a Godot addon. Keeping it in one place means a change to the
// capability vocabulary cannot leave a stub server silently advertising an
// outdated handshake that negotiation then rejects.
package gwptest

import (
	"encoding/json"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/version"
)

// Handshake returns the ping result a current-generation addon reports, with
// overrides applied on top. A nil override value deletes the key, which is how
// a pre-negotiation addon is expressed.
func Handshake(overrides map[string]any) json.RawMessage {
	payload := map[string]any{
		"status":            "ok",
		"engine":            "godot",
		"engine_version":    "4.4.1.stable",
		"stagehand_version": version.Version,
		"protocol_version":  gwp.ProtocolVersion,
		"protocol":          gwp.ProtocolID,
		"capabilities":      AllCapabilities(),
		"instance_token":    "abc123",
	}
	for key, value := range overrides {
		if value == nil {
			delete(payload, key)
			continue
		}
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return raw
}

// AllCapabilities returns every capability in the vocabulary, required first.
func AllCapabilities() []string {
	return append(append([]string{}, gwp.RequiredCapabilities...), gwp.OptionalCapabilities...)
}
