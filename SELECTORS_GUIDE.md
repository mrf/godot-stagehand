# Stagehand Selector System - Phase 2 Enhancement Guide

## Overview

This document outlines the enhanced selector grammar implemented in Stagehand, adding new capabilities like `text:`, `meta:`, `unique:` selectors, and `>>` chaining functionality. This enhancement allows for Playwright-like locators that enable more flexible UI element targeting.

## New Selector Types

### 1. Text-based Selection (`text:`)
Finds UI elements by their visible text content.
- Format: `text:The Button Text`
- Searches for nodes that display the specified text (labels, buttons, etc.)
- Useful for buttons, labels, headers with known text content

### 2. Metadata-based Selection (`meta:`)
Finds UI elements by metadata attributes similar to HTML `data-*` attributes.
- Format: `meta:key=value` for exact match
- Format: `meta:key` for existence check only  
- Looks for nodes that have the specified metadata key and optionally the value

### 3. Unique Element Identification (`unique:`)
Identifies semantically unique elements within their context.
- Format: `unique:some-unique-id`
- Finds elements that may have unique characteristics like:
  - Custom identifiers (UID, data-testid, etc.)
  - Elements that are semantically unique in their parent container

## Selector Chaining with `>>`

Elements can now be selected with chained queries using `>>`:
- Format: `selector1 >> selector2 >> selector3 ...`
- Each subsequent selector scopes its search to descendants of the previous results
- Allows for nested/relative selection patterns
- Example: `name:dialog >> text:Close` finds elements with text "Close" within a dialog named "dialog"

## Backwards Compatibility

All original selector types continue to work without changes:
- `name:` - Node name with optional wildcard matching
- `class:` - Node class/type matching  
- `group:` - Nodes within a specific group
- Absolute paths like `/root/MainUI/Button`

## Usage Examples

### Simple Selectors
```go
result, _ := ParseChain("text:Submit")        // Find by text
result, _ := ParseChain("meta:testId=loginBtn") // Find by metadata
result, _ := ParseChain("unique:primary-submit") // Find unique element
result, _ := ParseChain("class:Button")        // Original - by class
```

### Chained Selectors
```go
result, _ := ParseChain("name:form-container >> text:Submit")                // Form button
result, _ := ParseChain("group:menu-items >> class:MenuItem >> text:About")  // Nested menu item
result, _ := ParseChain("group:forms >> meta:purpose=search >> class:LineEdit") // Search field by metadata
```

## Implementation Details

### Go-side Parsing
- New functions: `ParseChain()` for multi-selector chains, `Parse()` for legacy backwards compatibility
- New selector types added: `Text`, `Meta`, `Unique`
- Maintains full backwards compatibility with existing selector formats

### GDScript Implementation (addons/stagehand/core/selector_engine.gd)
- Updated `query()`, `parse_chain()`, `_resolve_chain()` methods
- Added support for `_resolve_text()`, `_resolve_meta()`, `_resolve_unique()` 
- Implemented scoped chaining resolution (`_resolve_X_from()` variants)
- Text extraction implemented for common UI controls (Label, Button, etc.)

## Supported Node Types

### Text Extraction Targets
- Nodes with `get_text()` method (Label, Button, etc.)
- Nodes with text-related properties
- Placeholder text, tooltips included in search

### Metadata Support
- Any node property stored in Godot's metadata system accessible via `.has_meta()` and `.get_meta()`
- Commonly used for custom identifiers like data-testids

### Unique Element Recognition
- Nodes identified by specific metadata keys
- Semantically unique nodes in their parent context
- Custom unique identifier properties if present