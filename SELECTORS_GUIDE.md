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

### Text: `"text:Hello"` (loose) and `"text=Hello"` (exact)
Finds nodes that display the specified text content. This works with any control
that shows text labels, such as Label, Button, TextEdit, LineEdit, etc.

There are two matching modes:

- **`text:` — loose match (substring, case-insensitive).** Matches if the node's
  text *contains* the value, ignoring case. If the value contains glob
  metacharacters (`*`, `?`, `[`) it is instead treated as a glob pattern via
  `String.match()`. Convenient, but loose: `text:Continue` matches *both* a
  Button captioned "CONTINUE" *and* a Label reading "Continue from day 8?".
- **`text=` — exact match (whole string, case-sensitive).** Matches only when the
  node's text, after trimming surrounding whitespace, equals the value exactly.
  Use this to disambiguate a button caption from a descriptive label. Glob
  metacharacters are *not* interpreted in exact mode — they must match literally.

Examples:
- `text:Login` - any element whose text contains "login" (case-insensitive)
- `text:*Save*` - glob: elements whose text contains "Save"
- `text=CONTINUE` - only elements whose exact text is "CONTINUE"
- `text=Continue?` - only elements whose exact text is the literal "Continue?"

> **Note on the `?` glob caveat:** under `text:`, a trailing `?` is a single-char
> glob wildcard (so `text:Continue?` matches "Continued", "Continues", etc.).
> Use `text=Continue?` when you need the literal question mark.

#### Disambiguation on click

`godot_click` (and `godot_mouse_move`, and `godot_type_text`'s focus selector)
rank matches before acting: **interactive controls win**. The order is
`BaseButton` and its subclasses first, then other Controls that receive mouse
input (`mouse_filter != IGNORE`), then everything else — so a non-interactive
Label never gets clicked ahead of the real Button it shares a word with. The
`godot_click` result reports `matched` (how many nodes matched), `clicked_node`
(the path of the node actually clicked), and `ambiguous: true` when the selector
resolved to more than one node. Prefer `text=` or a chained/`name:`/`meta:`
selector when you need a guaranteed-unique target.

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

## Phase 3 Selector

### Role: `"role:button"`

Matches nodes by **accessibility role** rather than by concrete Godot class, so
one selector covers a family of widgets and survives a `Button` → `TextureButton`
refactor.

- `role:button` — any BaseButton-family control
- `role:check_box` / `role:check_button` — toggles
- `role:text_field` (LineEdit) / `role:multiline_text_field` (TextEdit, CodeEdit)
- `role:static_text` — Label, RichTextLabel
- `role:slider`, `role:spin_button`, `role:progress_indicator`, `role:tree`,
  `role:list`, `role:tab_bar`, `role:menu`, `role:dialog`, `role:image`, ...

Role names are the engine's own `DisplayServer.ROLE_*` constants, lowercased and
without the prefix. Matching is case-insensitive. Requires Godot 4.5+.

> **How roles are determined.** Godot's AccessKit integration is a *write-only*
> push API — the engine pushes roles into the platform screen reader and exposes
> no way to read them back from GDScript, and the accessibility element does not
> even exist unless a screen reader is attached. Stagehand therefore *derives*
> the role from the Control class hierarchy, mirroring what the engine does
> internally, and reports it in the engine's canonical vocabulary. Responses
> from `godot_get_accessibility_tree` carry `"source": "derived"` to make this
> explicit.

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

`"name:SettingsDialog >> role:button"` - every button inside the settings dialog
- Find elements displaying text "Profile" 
- Then within their child trees, locate the unique avatar container

### Notes:

1. Chained selectors support all the individual selector types mixed together
2. Path selectors in chains are interpreted relative to the parent selection when not absolute (not starting with "/")
3. The chain produces the final set of matched nodes after applying all constraints in sequence

## Backwards Compatibility

All original selectors continue to work exactly as before. Phase 2 enhancements are additive and do not break existing functionality.