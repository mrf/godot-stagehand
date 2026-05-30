package setup

import "strings"

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
// creating the section or the array as needed. Returns the (possibly unchanged)
// content and whether an edit was made.
func ensurePluginEnabled(content string) (string, bool) {
	if strings.Contains(content, pluginEntry) {
		return content, false
	}

	lines := strings.Split(content, "\n")
	start, end, found := findSection(lines, "editor_plugins")
	if found {
		for i := start + 1; i < end; i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "enabled=PackedStringArray(") {
				lines[i] = addToEnabledArray(lines[i])
				return strings.Join(lines, "\n"), true
			}
		}
		// Section present but no enabled= line — insert one after the header.
		newLines := insertAfter(lines, start, `enabled=PackedStringArray("`+pluginEntry+`")`)
		return strings.Join(newLines, "\n"), true
	}

	section := "[editor_plugins]\n\n" + `enabled=PackedStringArray("` + pluginEntry + `")` + "\n"
	return appendSection(content, section), true
}

// ensureAutoloadRegistered adds the StagehandServer autoload to the [autoload]
// section, creating the section if necessary. Returns the (possibly unchanged)
// content and whether an edit was made.
func ensureAutoloadRegistered(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), autoloadKey+"=") {
			return content, false
		}
	}

	start, _, found := findSection(lines, "autoload")
	if found {
		newLines := insertAfter(lines, start, autoloadLine)
		return strings.Join(newLines, "\n"), true
	}

	section := "[autoload]\n\n" + autoloadLine + "\n"
	return appendSection(content, section), true
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

// addToEnabledArray inserts pluginEntry into an existing
// enabled=PackedStringArray(...) line, handling both the empty and non-empty
// cases while preserving surrounding text.
func addToEnabledArray(line string) string {
	open := strings.Index(line, "(")
	closeIdx := strings.LastIndex(line, ")")
	if open == -1 || closeIdx == -1 || closeIdx < open {
		return line
	}
	inner := line[open+1 : closeIdx]
	entry := `"` + pluginEntry + `"`
	if strings.TrimSpace(inner) == "" {
		inner = entry
	} else {
		inner = strings.TrimRight(inner, " ") + ", " + entry
	}
	return line[:open+1] + inner + line[closeIdx:]
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
