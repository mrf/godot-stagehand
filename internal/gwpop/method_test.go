package gwpop

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The addon enforces the same blocklist; the two copies must not drift.
func TestBlockedMethodsMatchAddon(t *testing.T) {
	src, err := os.ReadFile("../../addons/stagehand/core/method_handler.gd")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)const BLOCKED_METHODS: PackedStringArray = \[(.*?)\]`).FindSubmatch(src)
	if block == nil {
		t.Fatal("BLOCKED_METHODS not found in method_handler.gd")
	}
	var addon []string
	for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(block[1], -1) {
		addon = append(addon, string(m[1]))
	}
	var gwpop []string
	for name := range blockedMethods {
		gwpop = append(gwpop, name)
	}
	slices.Sort(addon)
	slices.Sort(gwpop)
	if !slices.Equal(addon, gwpop) {
		t.Errorf("blocklists differ\naddon: %s\ngwpop: %s", strings.Join(addon, ", "), strings.Join(gwpop, ", "))
	}
}

// Dynamic-dispatch and reparenting methods would let a caller reach a blocked
// method indirectly (callv("free", [])) or restructure the tree.
func TestValidateMethodNameBlocksIndirection(t *testing.T) {
	for _, name := range []string{
		"call", "callv", "call_deferred", "set", "set_indexed", "propagate_call", "reparent",
	} {
		if err := ValidateMethodName(name); err == nil {
			t.Errorf("ValidateMethodName(%q) = nil, want blocked", name)
		}
	}
}
