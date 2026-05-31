package selector

import (
	"testing"
)

func TestParsePath(t *testing.T) {
	s, err := Parse("/root/UI/StartButton")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Type != Path {
		t.Errorf("type = %v, want Path", s.Type)
	}
	if s.Value != "/root/UI/StartButton" {
		t.Errorf("value = %q, want /root/UI/StartButton", s.Value)
	}
}

func TestParseName(t *testing.T) {
	s, err := Parse("name:*Button*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Type != Name {
		t.Errorf("type = %v, want Name", s.Type)
	}
	if s.Value != "*Button*" {
		t.Errorf("value = %q, want *Button*", s.Value)
	}
}

func TestParseClass(t *testing.T) {
	s, err := Parse("class:Button")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Type != Class {
		t.Errorf("type = %v, want Class", s.Type)
	}
	if s.Value != "Button" {
		t.Errorf("value = %q, want Button", s.Value)
	}
}

func TestParseGroup(t *testing.T) {
	s, err := Parse("group:interactive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Type != Group {
		t.Errorf("type = %v, want Group", s.Type)
	}
	if s.Value != "interactive" {
		t.Errorf("value = %q, want interactive", s.Value)
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("expected error for empty selector")
	}
}

func TestParseWhitespaceOnly(t *testing.T) {
	_, err := Parse("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only selector")
	}
}

func TestParseEmptyValue(t *testing.T) {
	for _, input := range []string{"name:", "class:", "group:"} {
		_, err := Parse(input)
		if err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestParseTrimsWhitespace(t *testing.T) {
	s, err := Parse("  class:Button  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Value != "Button" {
		t.Errorf("value = %q, want Button", s.Value)
	}
}

func TestParseText(t *testing.T) {
	s, err := ParseChain("text:Click Me")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*s) != 1 {
		t.Fatalf("expected chain with 1 item, got %d", len(*s))
	}
	selector := (*s)[0]
	if selector.Type != Text {
		t.Errorf("type = %v, want Text", selector.Type)
	}
	if selector.Value != "Click Me" {
		t.Errorf("value = %q, want Click Me", selector.Value)
	}
}

func TestParseMeta(t *testing.T) {
	s, err := ParseChain("meta:id=button-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*s) != 1 {
		t.Fatalf("expected chain with 1 item, got %d", len(*s))
	}
	selector := (*s)[0]
	if selector.Type != Meta {
		t.Errorf("type = %v, want Meta", selector.Type)
	}
	if selector.Value != "id=button-id" {
		t.Errorf("value = %q, want id=button-id", selector.Value)
	}
}

func TestParseUnique(t *testing.T) {
	s, err := ParseChain("unique:login-button")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*s) != 1 {
		t.Fatalf("expected chain with 1 item, got %d", len(*s))
	}
	selector := (*s)[0]
	if selector.Type != Unique {
		t.Errorf("type = %v, want Unique", selector.Type)
	}
	if selector.Value != "login-button" {
		t.Errorf("value = %q, want login-button", selector.Value)
	}
}

func TestParseChain(t *testing.T) {
	// Test simple chain
	s, err := ParseChain("name:Container >> text:Ok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*s) != 2 {
		t.Errorf("expected 2 selectors in chain, got %d", len(*s))
	}
	if (*s)[0].Type != Name || (*s)[0].Value != "Container" {
		t.Errorf("first selector = %v, %q; want Name, Container", (*s)[0].Type, (*s)[0].Value)
	}
	if (*s)[1].Type != Text || (*s)[1].Value != "Ok" {
		t.Errorf("second selector = %v, %q; want Text, Ok", (*s)[1].Type, (*s)[1].Value)
	}

	// Test backward compatibility with single selector
	s, err = ParseChain("class:Button")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*s) != 1 {
		t.Errorf("expected 1 selector in single case, got %d", len(*s))
	}
	if (*s)[0].Type != Class || (*s)[0].Value != "Button" {
		t.Errorf("single selector = %v, %q; want Class, Button", (*s)[0].Type, (*s)[0].Value)
	}
}

func TestParseChainErrors(t *testing.T) {
	tests := []struct {
		input     string
		wantError bool
	}{
		{"", true},
		{"  \t", true},                       // Whitespace only
		{"name:Button >>", true},             // Ends with >>
		{">> name:Button", true},             // Starts with >>
		{"name:Button >>  >> text:OK", true}, // Empty selector in chain
		{"name:", true},                      // Empty value
		{"text:", true},                      // Empty text value
		{"meta:", true},                      // Empty meta value
		{"unique:", true},                    // Empty unique value
	}

	for _, tc := range tests {
		_, err := ParseChain(tc.input)
		hasError := err != nil
		if hasError != tc.wantError {
			t.Errorf("ParseChain(%q): expected error = %v, got %v (error = %v)",
				tc.input, tc.wantError, hasError, err)
		}
	}
}

// TestParseLegacyMisclassifiesNewSelectors verifies that Parse() no longer
// silently coerces newer selector forms into wrong types.
func TestParseNewSelectorForms(t *testing.T) {
	tests := []struct {
		input     string
		wantType  Type
		wantValue string
	}{
		{"text:Click Me", Text, "Click Me"},
		{"meta:id=submit", Meta, "id=submit"},
		{"unique:login-button", Unique, "login-button"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			s, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.Type != tc.wantType {
				t.Errorf("type = %v, want %v (silent misclassification as path)", s.Type, tc.wantType)
			}
			if s.Value != tc.wantValue {
				t.Errorf("value = %q, want %q", s.Value, tc.wantValue)
			}
		})
	}
}

func TestTypeString(t *testing.T) {
	if Path.String() != "path" {
		t.Errorf("Path.String() = %q", Path.String())
	}
	if Class.String() != "class" {
		t.Errorf("Class.String() = %q", Class.String())
	}
	if Group.String() != "group" {
		t.Errorf("Group.String() = %q", Group.String())
	}
	if Name.String() != "name" {
		t.Errorf("Name.String() = %q", Name.String())
	}
	if Text.String() != "text" {
		t.Errorf("Text.String() = %q", Text.String())
	}
	if Meta.String() != "meta" {
		t.Errorf("Meta.String() = %q", Meta.String())
	}
	if Unique.String() != "unique" {
		t.Errorf("Unique.String() = %q", Unique.String())
	}
	typ := Type(99)
	if typ.String() != "SelectorType(99)" {
		t.Errorf("Type(99).String() = %q", typ.String())
	}
}
