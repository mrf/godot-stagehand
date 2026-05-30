package selector

import (
	"fmt"
)

// Demo function showing all new selector features
func Example() {
	// New selectors
	selectors := []string{
		"text:Submit",
		"meta:id=title",
		"unique:submit-button",
	}

	for _, s := range selectors {
		chain, err := ParseChain(s)
		if err != nil {
			fmt.Printf("Error parsing %s: %v\n", s, err)
			continue
		}
		fmt.Printf("%s parsed as %d element(s) in chain\n", s, len(*chain))
		if len(*chain) > 0 {
			fmt.Printf("  First element: type=%v, value='%s'\n", (*chain)[0].Type, (*chain)[0].Value)
		}
	}

	// Chained selectors
	chains := []string{
		"name:Container >> text:OK",
		"group:forms >> class:LineEdit >> unique:email-input",
	}

	fmt.Println("\nChained examples:")
	for _, s := range chains {
		chain, err := ParseChain(s)
		if err != nil {
			fmt.Printf("Error parsing %s: %v\n", s, err)
			continue
		}
		fmt.Printf("%s splits into %d parts\n", s, len(*chain))
		for i, selector := range *chain {
			fmt.Printf("  Part %d: type=%v, value='%s'\n", i, selector.Type, selector.Value)
		}
	}

	// Output:
	// text:Submit parsed as 1 element(s) in chain
	//   First element: type=text, value='Submit'
	// meta:id=title parsed as 1 element(s) in chain
	//   First element: type=meta, value='id=title'
	// unique:submit-button parsed as 1 element(s) in chain
	//   First element: type=unique, value='submit-button'
	//
	// Chained examples:
	// name:Container >> text:OK splits into 2 parts
	//   Part 0: type=name, value='Container'
	//   Part 1: type=text, value='OK'
	// group:forms >> class:LineEdit >> unique:email-input splits into 3 parts
	//   Part 0: type=group, value='forms'
	//   Part 1: type=class, value='LineEdit'
	//   Part 2: type=unique, value='email-input'
}
