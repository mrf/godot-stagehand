// Package selector parses and validates Stagehand selector strings on the Go side
type before they are sent to the Godot addon.
package selector

import (
	"fmt"
	"strings"
)

// Type identifies the kind of selector.
type Type int

const (
	Path Type = iota
	Name
	Class
	Group
)

func (t Type) String() string {
	switch t {
	case Path:
		return "path"
	case Name:
		return "name"
	case Class:
		return "class"
	case Group:
		return "group"
	}
	return fmt.Sprintf("SelectorType(%d)", int(t))
}

// Selector is a parsed, validated selector.
type Selector struct {
	Type  Type
	Value string
}

// Parse validates and parses a selector string.
// Returns an error for empty selectors or unsupported prefixes.
func Parse(s string) (*Selector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("selector is empty")
	}

	for _, prefix := range []struct {
		str  string
		typ  Type
	}{
		{"name:", Name},
		{"class:", Class},
		{"group:", Group},
	} {
		if strings.HasPrefix(s, prefix.str) {
			v := strings.TrimPrefix(s, prefix.str)
			v = strings.TrimSpace(v)
			if v == "" {
				return nil, fmt.Errorf("selector %q has empty value", prefix.str)
			}
			return &Selector{Type: prefix.typ, Value: v}, nil
		}
	}

	// No recognized prefix — treat as an exact node path.
	return &Selector{Type: Path, Value: s}, nil
}
