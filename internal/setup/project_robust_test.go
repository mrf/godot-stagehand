package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const multiLineEnabled = `config_version=5

[editor_plugins]

enabled=PackedStringArray(
"res://addons/other/plugin.cfg",
"res://addons/more/plugin.cfg"
)

[autoload]

Other="*res://other.gd"
`

func TestEnsurePluginEnabled_DifferentPluginContainingStagehandPath(t *testing.T) {
	// Contains pluginEntry as a substring but is a different plugin.
	in := "[editor_plugins]\n\n" +
		`enabled=PackedStringArray("res://addons/stagehand/plugin.cfg.bak")` + "\n"
	out, changed := ensurePluginEnabled(in)
	if !changed {
		t.Fatalf("different plugin matched as Stagehand; content unchanged:\n%s", out)
	}
	want := `enabled=PackedStringArray("res://addons/stagehand/plugin.cfg.bak", "` + pluginEntry + `")`
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\n%s", want, out)
	}
}

func TestEnsurePluginEnabled_EntryOutsideEnabledArrayIgnored(t *testing.T) {
	in := "[editor_plugins]\n\n; " + pluginEntry + "\nenabled=PackedStringArray()\n\n[misc]\n\nnote=\"" + pluginEntry + "\"\n"
	out, changed := ensurePluginEnabled(in)
	if !changed {
		t.Fatalf("entry outside enabled array counted as enabled:\n%s", out)
	}
	if !strings.Contains(out, `enabled=PackedStringArray("`+pluginEntry+`")`) {
		t.Errorf("plugin not added to enabled array\n%s", out)
	}
}

func TestEnsurePluginEnabled_MultiLineArray(t *testing.T) {
	out, changed := ensurePluginEnabled(multiLineEnabled)
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := countOccurrences(out, pluginEntry); got != 1 {
		t.Fatalf("plugin entry appears %d times, want 1\n%s", got, out)
	}
	want := "enabled=PackedStringArray(\n" +
		"\"res://addons/other/plugin.cfg\",\n" +
		"\"res://addons/more/plugin.cfg\",\n" +
		"\"" + pluginEntry + "\"\n)"
	if !strings.Contains(out, want) {
		t.Errorf("multi-line array not extended cleanly; want substring:\n%s\ngot:\n%s", want, out)
	}
	out2, changed2 := ensurePluginEnabled(out)
	if changed2 || out2 != out {
		t.Errorf("not idempotent on multi-line array\n%s", out2)
	}
}

func TestEnsurePluginEnabled_MultiLineAlreadyEnabled(t *testing.T) {
	in := "[editor_plugins]\n\nenabled=PackedStringArray(\n\"res://addons/other/plugin.cfg\",\n\"" + pluginEntry + "\"\n)\n"
	out, changed := ensurePluginEnabled(in)
	if changed || out != in {
		t.Errorf("already-enabled multi-line array modified (changed=%v)\n%s", changed, out)
	}
}

func TestEnsureAutoloadRegistered_StalePathIsRewritten(t *testing.T) {
	in := "[autoload]\n\nStagehandServer=\"*res://old/location/stagehand_server.gd\"\nOther=\"*res://other.gd\"\n"
	out, changed := ensureAutoloadRegistered(in)
	if !changed {
		t.Fatal("stale autoload path left untouched")
	}
	if got := countOccurrences(out, autoloadKey+"="); got != 1 {
		t.Errorf("autoload key appears %d times, want 1\n%s", got, out)
	}
	if strings.Contains(out, "old/location") {
		t.Errorf("stale path still present\n%s", out)
	}
	if !strings.Contains(out, autoloadLine) || !strings.Contains(out, `Other="*res://other.gd"`) {
		t.Errorf("expected current line and untouched neighbours\n%s", out)
	}
}

func TestEnsureAutoloadRegistered_KeyInOtherSectionIgnored(t *testing.T) {
	in := "[misc]\n\nStagehandServer=\"unrelated\"\n"
	out, changed := ensureAutoloadRegistered(in)
	if !changed || !strings.Contains(out, "[autoload]") || !strings.Contains(out, autoloadLine) {
		t.Errorf("autoload not registered when key only in another section (changed=%v)\n%s", changed, out)
	}
}

func TestWriteFileAtomic_ReplacesContentAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.godot")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files in dir: %v", entries)
	}
}

func TestWriteFileAtomic_FollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.godot")
	link := filepath.Join(dir, "project.godot")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeFileAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced by regular file")
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Errorf("target content = %q, want new", got)
	}
}

func TestWriteFileAtomic_FailureLeavesOriginalIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "project.godot")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := writeFileAtomic(path, []byte("new")); err == nil {
		t.Fatal("expected error writing into read-only dir")
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Errorf("original modified on failure: %q", got)
	}
}
