# Stagehand Selector Guide

This document describes the selector syntax supported by Stagehand for identifying UI elements in Godot scenes.

## Basic Selectors

These are the original selectors that were available from the start.

### Path: `"/root/UI/MyButton"`
Selects a specific node by its absolute path in the scene tree.

Example: `/root/MainPanel/LoginForm/UsernameInput`

### Name: `"name:*pattern*"`
Finds nodes whose names match a given pattern (supports globbing with `*` and `?`).

Examples:
- `name:Button` - finds nodes named exactly "Button"
- `name:*Button*` - finds nodes containing "Button" in the name
- `name:?Button` - finds nodes with a single character followed by "Button"

### Class: `"class:Button"`
Finds nodes that inherit from the specified class name.

Example: `class:Button` matches Button, BaseButton, MarginContainer, etc.

### Group: `"group:interactable"`
Finds nodes that are members of the specified group.

Example: `group:buttons` finds all nodes in the "buttons" group.

## Enhanced Phase 2 Selectors 

Additional selectors added in Phase 2 to better target dynamic content.

### Text: `"text:Hello"`
Finds nodes that display the specified text content. This works with any control that shows text labels, such as Label, Button, TextEdit, LineEdit, etc.

Examples:
- `text:Login` - finds elements showing the word "Login"
- `text:*Save*` - finds elements with text containing "Save"
- `text:Continue?` - finds elements showing "Continue?" (with question mark)

### Meta: `"meta:key=value"` or `"meta:someKey"`
Finds nodes based on metadata values attached to them. This includes exported variables, custom properties, or Godot's built-in metadata.

Examples:
- `meta:id=login_button` - finds element with id property equal to "login_button"
- `meta:role=primary` - finds elements with role property equal to "primary"
- `meta:itemId` - finds elements that have itemId property (any value)

### Unique: `"unique:submit-btn"`
Finds unique UI elements using heuristic matching of distinctive identifiers that should appear only once per screen.

Examples:
- `unique:logout-link` - targets a logout link typically unique on the page
- `unique:header-logo` - finds the site logo in header
- `unique:navigation-menu` - identifies the main navigation

## Chained Selectors: `"selector >> selector >> ..."`

The `>>` operator allows chaining selectors to narrow down the search scope. Each subsequent selector applies only to elements found by the previous selector.

Example chain breakdown:
1. First selector applies to the entire scene tree
2. Second selector searches only within elements found by the first
3. Third selector searches only within elements found by the second
4. And so on...

### Chaining Examples:

`"group:menu >> class:Button >> text:Settings"`
- First find all nodes in the "menu" group
- Then among those nodes and their children, find Button class instances  
- Finally among those and their children, find ones displaying the text "Settings"

`"class:VBoxContainer >> group:buttons >> name:*Submit*"`
- First find all VBoxContainers in the scene
- Then within each container, find members of the "buttons" group
- Finally match elements with names that contain "Submit"

`"text:Profile >> unique:avatar-container"`
- Find elements displaying text "Profile" 
- Then within their child trees, locate the unique avatar container

### Notes:

1. Chained selectors support all the individual selector types mixed together
2. Path selectors in chains are interpreted relative to the parent selection when not absolute (not starting with "/")
3. The chain produces the final set of matched nodes after applying all constraints in sequence

## Backwards Compatibility

All original selectors continue to work exactly as before. Phase 2 enhancements are additive and do not break existing functionality.