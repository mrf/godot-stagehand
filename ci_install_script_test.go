package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIInstallGodotExpandsHomeCachePath(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get repository root: %v", err)
	}

	testRoot := t.TempDir()
	fakeHome := filepath.Join(testRoot, "home")
	godotPath := filepath.Join(fakeHome, ".cache", "godot-ci", "Godot_v4.3-stable_linux.x86_64")
	if err := os.MkdirAll(filepath.Dir(godotPath), 0o755); err != nil {
		t.Fatalf("create cached Godot directory: %v", err)
	}
	if err := os.WriteFile(godotPath, []byte("cached godot"), 0o755); err != nil {
		t.Fatalf("create cached Godot binary: %v", err)
	}

	fakeBin := filepath.Join(testRoot, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("create fake bin directory: %v", err)
	}
	fakeCurl := filepath.Join(fakeBin, "curl")
	if err := os.WriteFile(fakeCurl, []byte("#!/bin/sh\necho unexpected download >&2\nexit 97\n"), 0o755); err != nil {
		t.Fatalf("create fake curl: %v", err)
	}

	cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "ci-install-godot.sh"))
	cmd.Dir = testRoot
	cmd.Env = []string{
		"HOME=" + fakeHome,
		"GODOT_CACHE_DIR=~/.cache/godot-ci",
		"GODOT_VERSION=4.3.stable",
		"PATH=" + fakeBin + ":/usr/bin:/bin",
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run installer with a home-relative cache path: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "GODOT_BIN="+godotPath) {
		t.Fatalf("installer did not export an absolute Godot path; output:\n%s", output)
	}
}

// ciInstallFakeCurl serves `curl ... -o <out> <url>` from $FAKE_RELEASE_DIR by
// the URL's basename, failing like `curl -f` on a missing file.
const ciInstallFakeCurl = `#!/bin/sh
out=
url=
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) out=$2; shift ;;
		--retry|--retry-delay) shift ;;
		-*) ;;
		*) url=$1 ;;
	esac
	shift
done
src="$FAKE_RELEASE_DIR/${url##*/}"
[ -f "$src" ] || { echo "curl: (22) 404 $url" >&2; exit 22; }
cp "$src" "$out"
`

// TestCIInstallGodotVerifiesDownloadAgainstSHA512SUMS runs the installer
// against a fake release directory holding a real zip and a SHA512-SUMS.txt
// in Godot's published "<hex>  <file>" format.
func TestCIInstallGodotVerifiesDownloadAgainstSHA512SUMS(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skipf("unzip not installed: %v", err)
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get repository root: %v", err)
	}
	const binName = "Godot_v4.6.2-stable_linux.x86_64"
	const zipName = binName + ".zip"
	zipBytes := ciInstallZip(t, binName, "#!/bin/sh\necho godot\n")
	sum := sha512.Sum512(zipBytes)
	good := hex.EncodeToString(sum[:])
	bad := strings.Repeat("0", len(good))

	cases := []struct {
		name     string
		sums     string
		omitSums bool
		wantErr  bool
	}{
		{name: "matching digest installs", sums: bad + "  Godot_v4.6.2-stable_linux.arm64.zip\n" + good + "  " + zipName + "\n"},
		{name: "mismatched digest fails", sums: bad + "  " + zipName + "\n", wantErr: true},
		{name: "no entry for the zip fails", sums: good + "  Godot_v4.6.2-stable_mono_linux_x86_64.zip\n", wantErr: true},
		{name: "missing SHA512-SUMS.txt fails", omitSums: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			releaseDir := filepath.Join(root, "release")
			fakeBin := filepath.Join(root, "bin")
			cacheDir := filepath.Join(root, "cache")
			for _, dir := range []string{releaseDir, fakeBin} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", dir, err)
				}
			}
			releaseContractWriteFile(t, filepath.Join(releaseDir, zipName), string(zipBytes))
			if !tc.omitSums {
				releaseContractWriteFile(t, filepath.Join(releaseDir, "SHA512-SUMS.txt"), tc.sums)
			}
			releaseContractWriteFile(t, filepath.Join(fakeBin, "curl"), ciInstallFakeCurl)

			cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "ci-install-godot.sh"))
			cmd.Dir = root
			cmd.Env = []string{
				"HOME=" + root,
				"GODOT_CACHE_DIR=" + cacheDir,
				"GODOT_VERSION=4.6.2.stable",
				"FAKE_RELEASE_DIR=" + releaseDir,
				"PATH=" + fakeBin + ":/usr/bin:/bin",
			}
			output, err := cmd.CombinedOutput()
			_, statErr := os.Stat(filepath.Join(cacheDir, binName))
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("installer: %v\n%s", err, output)
				}
				if statErr != nil {
					t.Fatalf("verified Godot binary not installed: %v\n%s", statErr, output)
				}
				return
			}
			if err == nil {
				t.Fatalf("installer accepted an unverified download; output:\n%s", output)
			}
			if statErr == nil {
				t.Errorf("installer left an unverified Godot binary in the cache")
			}
			if _, err := os.Stat(filepath.Join(cacheDir, zipName)); err == nil {
				t.Errorf("installer left the unverified zip in the cache")
			}
		})
	}
}

func ciInstallZip(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create(name)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
