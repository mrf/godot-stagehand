// Package selector parses and validates Stagehand selector strings on the Go side
// before they are sent to the Godot addon.
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
	Text      // text: for label text content matching, substring + case-insensitive (Phase 2)
	Meta      // meta: for metadata attribute matching (Phase 2)
	Unique    // unique: for unique UI element identification (Phase 2)
	TextExact // text= for exact (whitespace-trimmed, case-sensitive) label text matching
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
	case Text:
		return "text"
	case Meta:
		return "meta"
	case Unique:
		return "unique"
	case TextExact:
		return "text_exact"
	}
	return fmt.Sprintf("SelectorType(%d)", int(t))
}

// Selector is a parsed, validated selector.
type Selector struct {
	Type  Type
	Value string
}

// Chain represents a sequence of selectors chained together with >>
type Chain []*Selector

// Parse parses a single selector string. It now routes through the full grammar
// (same as ParseChain) so newer selector forms (text:, meta:, unique:) are
// correctly classified rather than silently coerced to a path selector.
func Parse(s string) (*Selector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("selector is empty")
	}
	return parseSingleSelectorAllTypes(s)
}

// ParseChain parses a selector string that may contain chained selectors.
// Kept separate to maintain existing API compatibility
func ParseChain(s string) (*Chain, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("selector is empty")
	}

	// Split by >> for chaining, handling potential leading/trailing whitespace
	chainParts := strings.Split(s, ">>")
	chain := make(Chain, 0, len(chainParts))

	for i, part := range chainParts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("selector chain at index %d is empty", i)
		}

		selector, err := parseSingleSelectorAllTypes(part)
		if err != nil {
			return nil, err
		}
		chain = append(chain, selector)
	}

	return &chain, nil
}

// parseSingleSelectorAllTypes parses a single pre-trimmed, non-empty selector.
// Callers (Parse, ParseChain) own the trim/empty guard.
func parseSingleSelectorAllTypes(s string) (*Selector, error) {
	for _, prefix := range []struct {
		str string
		typ Type
	}{
		{"name:", Name},
		{"class:", Class},
		{"group:", Group},
		{"text=", TextExact},
		{"text:", Text},
		{"meta:", Meta},
		{"unique:", Unique},
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
