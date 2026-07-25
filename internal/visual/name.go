package visual

import (
	"fmt"
	"path/filepath"
	"strings"
)

// maxNameLen bounds a baseline name well under the 255-byte component limit
// common to ext4/APFS/NTFS, leaving room for the "-actual.png" suffix.
const maxNameLen = 128

// NameSyntax is the human-readable form of the baseline-name allowlist, quoted
// in tool descriptions and error messages so a caller can fix a rejected name
// without reading this file.
const NameSyntax = "ASCII letters, digits, '_', '-' and '.', starting and ending with a letter or digit, " +
	"no '..' sequence, at most 128 characters"

// windowsReservedNames are device names that Windows resolves regardless of
// the directory or extension, so <name>.png would not be a file at all.
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidateName checks a baseline name against the documented allowlist. The
// name is used verbatim as a filename stem, so it is validated by what it may
// contain rather than by blocklisting known-bad sequences: anything outside
// NameSyntax is rejected. That excludes path separators (both platforms'),
// absolute and drive-relative paths, dot segments, control characters,
// whitespace and non-ASCII text in one rule.
//
// Scenario files are data and may arrive from a pull request; a step must not
// be able to name a file outside the run's directories.
func ValidateName(name string) error {
	reject := func(reason string) error {
		return fmt.Errorf("invalid baseline name %q: %s (allowed: %s)", name, reason, NameSyntax)
	}
	if name == "" {
		return fmt.Errorf("baseline name is required (allowed: %s)", NameSyntax)
	}
	if len(name) > maxNameLen {
		return reject(fmt.Sprintf("%d characters exceeds the %d-character limit", len(name), maxNameLen))
	}
	for _, r := range name {
		if !isNameRune(r) {
			return reject(fmt.Sprintf("character %q is not allowed", r))
		}
	}
	if !isAlphanumeric(rune(name[0])) || !isAlphanumeric(rune(name[len(name)-1])) {
		return reject("must start and end with a letter or digit")
	}
	if strings.Contains(name, "..") {
		return reject("must not contain a '..' sequence")
	}
	if windowsReservedNames[strings.ToLower(name)] {
		return reject("is a reserved device name on Windows")
	}
	return nil
}

func isNameRune(r rune) bool {
	return isAlphanumeric(r) || r == '_' || r == '-' || r == '.'
}

func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// containedPath joins file onto dir and proves the result is a direct child of
// dir. ValidateName already rejects anything that could escape, so a failure
// here means a caller bypassed validation — belt-and-braces against the join
// itself being the last line of defence.
func containedPath(dir, file string) (string, error) {
	joined := filepath.Join(dir, file)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve directory %q: %w", dir, err)
	}
	absPath, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", joined, err)
	}
	if filepath.Dir(absPath) != absDir {
		return "", fmt.Errorf("refusing to write %q: resolves outside %q", joined, dir)
	}
	return joined, nil
}

// baselinePath resolves <dir>/<name>.png for a validated name.
func baselinePath(dir, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return containedPath(dir, name+".png")
}
