package selector

import (
	"fmt"
	"testing"
)

// TestParseNewSelectors tests the new text:, meta:, unique: selectors
func TestParseNewSelectors(t *testing.T) {
	tests := []struct {
		input    string
		expected Type
		wantErr  bool
	}{
		{"text:Submit", Text, false},
		{"meta:id=submit_btn", Meta, false},
		{"meta:customAttr", Meta, false},
		{"unique:submit-btn", Unique, false},
		{"name:MyButton", Name, false},
		{"class:Button", Class, false},
		{"group:buttons", Group, false},
		{"/absolute/path", Path, false},
		{"relativePath", Path, false},
		// Error cases
		{"text:", Text, true},     // Empty value
		{"meta:", Meta, true},     // Empty value
		{"unique:", Unique, true}, // Empty value
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input_%s", tt.input), func(t *testing.T) {
			// For single selectors use ParseChain since the old Parse function doesn't support new types
			result, err := ParseChain(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseChain(%q) = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChain(%q) = %v, %v", tt.input, result, err)
			}

			if len(*result) != 1 {
				t.Errorf("Expected 1 selector in chain, got %d", len(*result))
			}

			singleResult := (*result)[0]
			if singleResult.Type != tt.expected {
				t.Errorf("ParseChain(%q) = %v, want %v", tt.input, singleResult.Type, tt.expected)
			}
		})
	}
}

// TestParseChaining tests the >> chaining functionality
func TestParseChaining(t *testing.T) {
	tests := []struct {
		input     string
		chainLen  int
		firstType Type
		lastType  Type
	}{
		{"name:dialog >> text:Cancel", 2, Name, Text},
		{"group:forms >> class:LineEdit >> unique:email", 3, Group, Unique},
		{"class:VBoxContainer >> text:Settings >> name:advanced_options", 3, Class, Name},
		// Single selector should still work
		{"name:single", 1, Name, Name},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseChain(tt.input)
			if err != nil {
				t.Fatalf("ParseChain(%q) = %v, want no error", tt.input, err)
			}

			if len(*result) != tt.chainLen {
				t.Errorf("Expected chain length %d, got %d", tt.chainLen, len(*result))
			}

			if len(*result) > 0 {
				if (*result)[0].Type != tt.firstType {
					t.Errorf("First selector should be %v, got %v", tt.firstType, (*result)[0].Type)
				}
				if (*result)[len(*result)-1].Type != tt.lastType {
					t.Errorf("Last selector should be %v, got %v", tt.lastType, (*result)[len(*result)-1].Type)
				}
			}
		})
	}
}

// TestParseChainingErrors tests error cases for chaining
func TestParseChainingErrors(t *testing.T) {
	tests := []struct {
		input string
	}{
		{">> name:button"},
		{"name:button >>"},
		{"name:button >>  >> text:ok"}, // Empty middle portion
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseChain(tt.input)
			if err == nil {
				t.Errorf("ParseChain(%q) = nil, want error", tt.input)
			}
		})
	}
}

// TestBackwardCompatibility ensures old selectors still work
func TestBackwardCompatibility(t *testing.T) {
	tests := []struct {
		input    string
		expected Type
		value    string
	}{
		{"name:*Close*", Name, "*Close*"},
		{"class:Window", Class, "Window"},
		{"group:interactive", Group, "interactive"},
		{"/root/GUI/MainButton", Path, "/root/GUI/MainButton"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) = %v, want no error", tt.input, err)
			}

			// Old Parse returns individual Selectors, not chains
			if result.Type != tt.expected || result.Value != tt.value {
				t.Errorf("Parse(%q) = Type: %v, Value: %q, want Type: %v, Value: %q",
					tt.input, result.Type, result.Value, tt.expected, tt.value)
			}
		})
	}
}

// Example tests to demonstrate usage
func ExampleParse() {
	// Parse single selector (backward compatible)
	selector1, _ := Parse("name:myButton")
	fmt.Printf("Old syntax: %s=%s\n", selector1.Type.String(), selector1.Value)

	// Chained usage with ParseChain (new functionality)
	chain, _ := ParseChain("group:forms >> text:Email >> unique:email_input")
	fmt.Printf("Parsed %d chained selectors\n", len(*chain))

	// Output:
	// Old syntax: name=myButton
	// Parsed 3 chained selectors
}
