package setup

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// versionedAddon returns an in-memory embedded addon tree reporting the given
// VERSION in stagehand_version.gd.
func versionedAddon(version string) fstest.MapFS {
	return fstest.MapFS{
		"plugin.cfg": {Data: []byte("[plugin]\nname=\"Stagehand\"\n")},
		"stagehand_version.gd": {Data: []byte(
			"extends RefCounted\nconst VERSION: String = \"" + version + "\"\nconst PROTOCOL_VERSION: int = 1\n",
		)},
	}
}

// writeInstalledVersion writes a stagehand_version.gd reporting version into
// dir, simulating a previously-installed addon copy. An empty version writes
// a file with no VERSION line at all, simulating an unparseable/corrupt copy.
func writeInstalledVersion(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "extends RefCounted\n// no version here\n"
	if version != "" {
		content = "extends RefCounted\nconst VERSION: String = \"" + version + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "stagehand_version.gd"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompareAddonVersions_Stale(t *testing.T) {
	dest := t.TempDir()
	writeInstalledVersion(t, dest, "0.2.0")
	installed, embedded, stale := compareAddonVersions(versionedAddon("0.3.1"), dest)
	if !stale {
		t.Fatalf("expected stale, installed=%q embedded=%q", installed, embedded)
	}
	if installed != "0.2.0" || embedded != "0.3.1" {
		t.Errorf("installed=%q embedded=%q", installed, embedded)
	}
}

func TestCompareAddonVersions_Match(t *testing.T) {
	dest := t.TempDir()
	writeInstalledVersion(t, dest, "0.3.1")
	_, _, stale := compareAddonVersions(versionedAddon("0.3.1"), dest)
	if stale {
		t.Error("expected not stale when versions match")
	}
}

func TestCompareAddonVersions_InstalledNewer(t *testing.T) {
	dest := t.TempDir()
	writeInstalledVersion(t, dest, "0.4.0")
	_, _, stale := compareAddonVersions(versionedAddon("0.3.1"), dest)
	if stale {
		t.Error("expected not stale when installed is newer than embedded")
	}
}

func TestCompareAddonVersions_UnparseableInstalledDegradesGracefully(t *testing.T) {
	dest := t.TempDir()
	writeInstalledVersion(t, dest, "") // no VERSION line at all
	_, _, stale := compareAddonVersions(versionedAddon("0.3.1"), dest)
	if stale {
		t.Error("expected not stale when installed version is unparseable")
	}
}

func TestCompareAddonVersions_MissingInstalledFile(t *testing.T) {
	dest := t.TempDir() // no stagehand_version.gd at all
	_, _, stale := compareAddonVersions(versionedAddon("0.3.1"), dest)
	if stale {
		t.Error("expected not stale when installed stagehand_version.gd is missing")
	}
}

func TestCompareAddonVersions_UnparseableEmbeddedDegradesGracefully(t *testing.T) {
	dest := t.TempDir()
	writeInstalledVersion(t, dest, "0.2.0")
	_, _, stale := compareAddonVersions(versionedAddon(""), dest)
	if stale {
		t.Error("expected not stale when embedded version is unparseable")
	}
}
