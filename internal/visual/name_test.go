package visual

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

func TestValidateNameAcceptsExistingSafeNames(t *testing.T) {
	// Every name the docs and shipped scenarios already use must keep working;
	// tightening validation must not invalidate a repository of baselines.
	for _, name := range []string{
		"main_menu", "hud", "hud_full", "settings", "world",
		"main-menu", "menu.1080p", "a", "Main_Menu_1920x1080", "9lives",
		strings.Repeat("n", maxNameLen),
	} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateNameRejectsUnsafeNames(t *testing.T) {
	cases := map[string]string{
		"empty":               "",
		"dot":                 ".",
		"dotdot":              "..",
		"parent traversal":    "../escape",
		"nested traversal":    "a/../../escape",
		"unix separator":      "sub/menu",
		"windows separator":   `sub\menu`,
		"windows traversal":   `..\escape`,
		"absolute unix":       "/etc/passwd",
		"absolute windows":    `C:\Windows\system32`,
		"drive relative":      "C:menu",
		"leading dot":         ".hidden",
		"trailing dot":        "menu.",
		"leading dash":        "-menu",
		"trailing dash":       "menu-",
		"embedded dotdot":     "menu..png",
		"nul byte":            "menu\x00.png",
		"newline":             "menu\nname",
		"tab":                 "menu\tname",
		"del control":         "menu\x7f",
		"space":               "main menu",
		"tilde":               "menu~1",
		"unicode":             "menü",
		"too long":            strings.Repeat("n", maxNameLen+1),
		"windows device":      "CON",
		"windows device case": "com1",
	}
	for label, name := range cases {
		if err := ValidateName(name); err == nil {
			t.Errorf("%s: ValidateName(%q) = nil, want an error", label, name)
		}
	}
}

func TestValidateNameErrorNamesTheOffender(t *testing.T) {
	err := ValidateName("../escape")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `"../escape"`) {
		t.Errorf("error %q does not quote the rejected name", err)
	}
}

func TestBaselinePathIsContained(t *testing.T) {
	dir := t.TempDir()
	got, err := baselinePath(dir, "main_menu")
	if err != nil {
		t.Fatalf("baselinePath: %v", err)
	}
	if want := filepath.Join(dir, "main_menu.png"); got != want {
		t.Errorf("baselinePath = %q, want %q", got, want)
	}
}

func TestBaselinePathRejectsEveryUnsafeName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", "..", "../escape", `..\escape`, "/etc/passwd", "sub/menu", ".hidden"} {
		if _, err := baselinePath(dir, name); err == nil {
			t.Errorf("baselinePath(%q) = nil error, want rejection", name)
		}
	}
}

// TestArtifactPathsAreContained proves the diff artifacts land inside the
// configured artifact directory rather than merely being joined onto it.
func TestArtifactPathsAreContained(t *testing.T) {
	dir := t.TempDir()
	for _, suffix := range []string{"-actual.png", "-diff.png"} {
		got, err := containedPath(dir, "main_menu"+suffix)
		if err != nil {
			t.Fatalf("containedPath(%q): %v", suffix, err)
		}
		if filepath.Dir(got) != filepath.Clean(dir) {
			t.Errorf("containedPath = %q, escapes %q", got, dir)
		}
	}
}

func TestContainedPathRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"../escape.png", "sub/escape.png", "/tmp/escape.png"} {
		if _, err := containedPath(dir, file); err == nil {
			t.Errorf("containedPath(%q) = nil error, want rejection", file)
		}
	}
}

func TestCompareBaselineRejectsUnsafeName(t *testing.T) {
	shot := Shot{PNG: visualtest.SolidPNG(1, 1, visualtest.Opaque), Width: 1, Height: 1}
	_, err := CompareBaseline(DiffConfig{
		BaselineDir: t.TempDir(),
		ArtifactDir: t.TempDir(),
		Name:        "../escape",
	}, shot)
	if err == nil {
		t.Fatal("CompareBaseline accepted a path-traversing baseline name")
	}
}

// TestSaveBaselineNeverCreatesDirForBadName guards the ordering: validation
// must reject before any filesystem mutation happens.
func TestSaveBaselineNeverCreatesDirForBadName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "baselines")
	shot := Shot{PNG: visualtest.SolidPNG(1, 1, visualtest.Opaque), Width: 1, Height: 1}
	if _, err := SaveBaseline(dir, "../escape", "", shot); err == nil {
		t.Fatal("SaveBaseline accepted a path-traversing baseline name")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("baseline directory was created despite an invalid name: %v", err)
	}
}
