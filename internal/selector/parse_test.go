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
	typ := Type(99)
	if typ.String() != "SelectorType(99)" {
		t.Errorf("Type(99).String() = %q", typ.String())
	}
}
