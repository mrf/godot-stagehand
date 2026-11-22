package setup

import (
	"slices"
	"strings"
)

// These constants describe the project.godot entries Stagehand needs. The
// autoload registration is required at runtime because plugin.gd only registers
// the autoload while the Godot editor is open.
const (
	// pluginEntry is the value added to the editor_plugins enabled array.
	pluginEntry = "res://addons/stagehand/plugin.cfg"
	// autoloadKey is the autoload singleton name registered in [autoload].
	autoloadKey = "StagehandServer"
	// autoloadLine is the full project.godot line registering the autoload.
	autoloadLine = `StagehandServer="*res://addons/stagehand/autoload/stagehand_server.gd"`
)

// ConfigureProject applies both required edits to project.godot content: it
// enables the Stagehand editor plugin and registers the StagehandServer
// autoload. It is idempotent — applying it to already-configured content
// returns the content unchanged with changed=false.
func ConfigureProject(content string) (result string, changed bool) {
	out, pluginChanged := ensurePluginEnabled(content)
	out, autoloadChanged := ensureAutoloadRegistered(out)
	return out, pluginChanged || autoloadChanged
}

// ensurePluginEnabled adds pluginEntry to the [editor_plugins] enabled array,
// creating the section or the array as needed. The array may span several
// lines, and only an exact string element of that array counts as already
// enabled. Returns the (possibly unchanged) content and whether an edit was
// made.
func ensurePluginEnabled(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	start, end, found := findSection(lines, "editor_plugins")
	if !found {
		section := "[editor_plugins]\n\n" + `enabled=PackedStringArray("` + pluginEntry + `")` + "\n"
		return appendSection(content, section), true
	}

	for i := start + 1; i < end; i++ {
		if !isKeyLine(lines[i], "enabled") {
			continue
		}
		last, joined, ok := arrayExtent(lines, i)
		if !ok {
			return content, false // unbalanced value: don't guess
		}
		if slices.Contains(quotedStrings(joined), pluginEntry) {
			return content, false
		}
		lines = slices.Replace(lines, i, last+1, strings.Split(appendToArray(joined), "\n")...)
		return strings.Join(lines, "\n"), true
	}

	// Section present but no enabled= line — insert one after the header.
	newLines := insertAfter(lines, start, `enabled=PackedStringArray("`+pluginEntry+`")`)
	return strings.Join(newLines, "\n"), true
}

// ensureAutoloadRegistered makes the [autoload] section carry autoloadLine for
// the StagehandServer key: a missing key is added (creating the section if
// necessary) and a stale value is rewritten in place. Returns the (possibly
// unchanged) content and whether an edit was made.
func ensureAutoloadRegistered(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	start, end, found := findSection(lines, "autoload")
	if !found {
		section := "[autoload]\n\n" + autoloadLine + "\n"
		return appendSection(content, section), true
	}

	for i := start + 1; i < end; i++ {
		if !isKeyLine(lines[i], autoloadKey) {
			continue
		}
		if strings.TrimSpace(lines[i]) == autoloadLine {
			return content, false
		}
		cr := ""
		if strings.HasSuffix(lines[i], "\r") {
			cr = "\r"
		}
		lines[i] = autoloadLine + cr
		return strings.Join(lines, "\n"), true
	}

	newLines := insertAfter(lines, start, autoloadLine)
	return strings.Join(newLines, "\n"), true
}

// isKeyLine reports whether line is a `key=value` assignment for key.
func isKeyLine(line, key string) bool {
	k, _, ok := strings.Cut(line, "=")
	return ok && strings.TrimSpace(k) == key
}

// arrayExtent finds where the parenthesised value starting on lines[first]
// ends, ignoring parens inside quoted strings. It returns the index of the
// last line and the text of lines[first:last+1] joined by newlines. ok is
// false when the parens never balance.
func arrayExtent(lines []string, first int) (last int, joined string, ok bool) {
	depth, inQuote, escaped, opened := 0, false, false, false
	for i := first; i < len(lines); i++ {
		text := lines[i]
		if i == first {
			_, text, _ = strings.Cut(text, "=")
		}
		for _, r := range text {
			switch {
			case escaped:
				escaped = false
			case inQuote && r == '\\':
				escaped = true
			case r == '"':
				inQuote = !inQuote
			case inQuote:
			case r == '(':
				depth++
				opened = true
			case r == ')':
				depth--
			}
		}
		if opened && depth == 0 {
			return i, strings.Join(lines[first:i+1], "\n"), true
		}
	}
	return 0, "", false
}

// quotedStrings returns the contents of every double-quoted string in s.
func quotedStrings(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, escaped := false, false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case inQuote && r == '\\':
			escaped = true
		case r == '"':
			if inQuote {
				out = append(out, cur.String())
				cur.Reset()
			}
			inQuote = !inQuote
		case inQuote:
			cur.WriteRune(r)
		}
	}
	return out
}

// findSection locates the [name] section header in lines and returns the index
// of the header line (start) and the index of the next section header or len
// (end, exclusive). found is false when the section is absent.
func findSection(lines []string, name string) (start, end int, found bool) {
	header := "[" + name + "]"
	start = -1
	for i, l := range lines {
		if strings.TrimSpace(l) == header {
			start = i
			break
		}
	}
	if start == -1 {
		return -1, -1, false
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			end = i
			break
		}
	}
	return start, end, true
}

// insertAfter returns a new slice with value inserted immediately after index i.
func insertAfter(lines []string, i int, value string) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:i+1]...)
	out = append(out, value)
	out = append(out, lines[i+1:]...)
	return out
}

// appendToArray adds pluginEntry as the last element of the array text
// (`enabled=PackedStringArray(...)`, possibly multi-line). When the array
// already spans lines the new element goes on its own line.
func appendToArray(text string) string {
	open := strings.Index(text, "(")
	closeIdx := strings.LastIndex(text, ")")
	if open == -1 || closeIdx < open {
		return text
	}
	inner := text[open+1 : closeIdx]
	body := strings.TrimRight(inner, " \t\r\n")
	trailing := inner[len(body):]
	entry := `"` + pluginEntry + `"`

	switch {
	case strings.TrimSpace(body) == "":
		body, trailing = entry, ""
	case !strings.Contains(inner, "\n"):
		body += ", " + entry
	default:
		sep := ",\n"
		if strings.Contains(inner, "\r\n") {
			sep = ",\r\n"
		}
		if strings.HasSuffix(body, ",") {
			sep = sep[1:] // already has a trailing comma
		}
		body += sep + entry
	}
	return text[:open+1] + body + trailing + text[closeIdx:]
}

// appendSection appends a section to content, ensuring it is separated from the
// preceding content by a blank line and that the result ends with a newline.
func appendSection(content, section string) string {
	s := content
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += "\n" + section
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}
