package setup

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// addonVersionPattern matches stagehand_version.gd's `const VERSION: String =
// "..."` line, mirroring the pattern internal/version's own drift tests use.
var addonVersionPattern = regexp.MustCompile(`(?m)^const VERSION: String = "([^"]*)"`)

// compareAddonVersions reports the VERSION declared by the addon already
// installed at destDir versus the VERSION embedded in this binary (addonFS),
// and whether the installed copy is older.
//
// Any failure to read or parse either side (missing file, no VERSION line,
// not a MAJOR.MINOR.PATCH string) degrades to stale=false rather than
// erroring — an unreadable version is not evidence of staleness, and setup
// must never crash or wedge over it.
func compareAddonVersions(addonFS fs.FS, destDir string) (installed, embedded string, stale bool) {
	installed, installedOK := readAddonVersion(os.DirFS(destDir))
	embedded, embeddedOK := readAddonVersion(addonFS)
	if !installedOK || !embeddedOK {
		return installed, embedded, false
	}
	cmp, err := compareSemver(installed, embedded)
	if err != nil {
		return installed, embedded, false
	}
	return installed, embedded, cmp < 0
}

func readAddonVersion(fsys fs.FS) (string, bool) {
	data, err := fs.ReadFile(fsys, "stagehand_version.gd")
	if err != nil {
		return "", false
	}
	match := addonVersionPattern.FindStringSubmatch(string(data))
	if match == nil {
		return "", false
	}
	return match[1], true
}

// compareSemver returns -1, 0, or 1 as a compares before, equal to, or after
// b, treating both as MAJOR.MINOR.PATCH. It errors if either fails to parse.
func compareSemver(a, b string) (int, error) {
	pa, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemver(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("not a MAJOR.MINOR.PATCH version: %q", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, fmt.Errorf("invalid version component %q in %q", p, v)
		}
		out[i] = n
	}
	return out, nil
}
