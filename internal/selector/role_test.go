package selector

import "testing"

func TestParseRole(t *testing.T) {
	selector, err := Parse("role:button")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if selector.Type != Role {
		t.Errorf("type = %v, want Role", selector.Type)
	}
	if selector.Value != "button" {
		t.Errorf("value = %q, want %q", selector.Value, "button")
	}
}

func TestRoleTypeString(t *testing.T) {
	if Role.String() != "role" {
		t.Errorf("Role.String() = %q, want %q", Role.String(), "role")
	}
}

func TestParseRoleEmptyValue(t *testing.T) {
	if _, err := Parse("role:"); err == nil {
		t.Error("Parse(\"role:\") should error on an empty value")
	}
	if _, err := Parse("role:   "); err == nil {
		t.Error("Parse(\"role:   \") should error on a whitespace-only value")
	}
}

// role: participates in >> chaining like every other prefix.
func TestParseChainWithRole(t *testing.T) {
	chain, err := ParseChain("name:Dialog >> role:button")
	if err != nil {
		t.Fatalf("ParseChain() error = %v", err)
	}
	if len(*chain) != 2 {
		t.Fatalf("chain length = %d, want 2", len(*chain))
	}
	if (*chain)[1].Type != Role {
		t.Errorf("chain[1].Type = %v, want Role", (*chain)[1].Type)
	}
	if (*chain)[1].Value != "button" {
		t.Errorf("chain[1].Value = %q, want %q", (*chain)[1].Value, "button")
	}
}

// A role: selector must not be silently coerced into a path selector, which is
// what the fallthrough branch would do if the prefix were not registered.
func TestParseRoleIsNotPath(t *testing.T) {
	for _, s := range []string{"role:check_box", "role:text_field", "role:slider"} {
		sel, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", s, err)
		}
		if sel.Type == Path {
			t.Errorf("Parse(%q) fell through to Path", s)
		}
	}
}
