package hostcompat

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exit codes of scripts/hostcompat-checkout.sh. The doc (Amendment 4) classes a
// checkout or SHA-mismatch failure as infrastructure — retry, never a compat
// failure — so that code must be distinct from every other outcome, including
// a usage error and the "pin not resolved yet" report.
const (
	checkoutOK       = 0
	checkoutUsage    = 2
	checkoutUnpinned = 3
	checkoutInfra    = 75
)

// fixtureRepo is a throwaway upstream: two commits, the first tagged v1.
type fixtureRepo struct {
	dir    string
	first  string
	second string
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false",
	}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newFixtureRepo(t *testing.T) fixtureRepo {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(dir, "project.godot"), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	write("; one\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "one")
	first := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "tag", "-a", "v1", "-m", "v1")
	write("; two\n")
	gitIn(t, dir, "commit", "-q", "-am", "two")
	return fixtureRepo{dir: dir, first: first, second: gitIn(t, dir, "rev-parse", "HEAD")}
}

// runCheckout runs the script against a manifest naming one project and
// returns its stdout, stderr and exit code. The destination root is a fresh
// temp dir unless the caller passes one.
func runCheckout(t *testing.T, dest, repo, ref, commit string, args ...string) (string, string, int) {
	t.Helper()
	for _, tool := range []string{"git", "jq", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available: %v", tool, err)
		}
	}
	commitJSON := "null"
	if commit != "" {
		commitJSON = fmt.Sprintf("%q", commit)
	}
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	body := fmt.Sprintf(`{"schema_version":1,"projects":[{"id":"fixture","repo":%q,"ref":%q,"commit":%s}]}`,
		repo, ref, commitJSON)
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if len(args) == 0 {
		args = []string{"fixture"}
	}

	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(t), "scripts", "hostcompat-checkout.sh")}, args...)...)
	cmd.Env = append(os.Environ(),
		"HOSTCOMPAT_MANIFEST="+manifest,
		"HOSTCOMPAT_DEST_DIR="+dest,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run script: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return strings.TrimSpace(stdout.String()), stderr.String(), code
}

func headOf(t *testing.T, dir string) string { return gitIn(t, dir, "rev-parse", "HEAD") }

func TestCheckoutClonesAtPinnedCommitAndPrintsPath(t *testing.T) {
	repo := newFixtureRepo(t)
	dest := t.TempDir()

	stdout, stderr, code := runCheckout(t, dest, "file://"+repo.dir, "main", repo.first)
	if code != checkoutOK {
		t.Fatalf("exit = %d, want %d\n%s", code, checkoutOK, stderr)
	}
	want := filepath.Join(dest, "fixture")
	if stdout != want {
		t.Errorf("stdout = %q, want the checkout path %q", stdout, want)
	}
	// The pin is the first commit even though main has moved on: the commit,
	// not the ref, is the authority.
	if got := headOf(t, want); got != repo.first {
		t.Errorf("HEAD = %s, want pinned %s", got, repo.first)
	}
}

func TestCheckoutIsIdempotentWhenAlreadyAtPin(t *testing.T) {
	repo := newFixtureRepo(t)
	dest := t.TempDir()
	if _, stderr, code := runCheckout(t, dest, "file://"+repo.dir, "main", repo.first); code != checkoutOK {
		t.Fatalf("first run exit = %d\n%s", code, stderr)
	}
	if _, stderr, code := runCheckout(t, dest, "file://"+repo.dir, "main", repo.first); code != checkoutOK {
		t.Errorf("second run exit = %d, want %d (a restored cache must be reusable)\n%s", code, checkoutOK, stderr)
	}
}

// A checkout whose HEAD is not the pin is the SHA-mismatch case: a stale cache
// or a hand-edited pin. It is infrastructure, and the script must not quietly
// reset a directory it did not just create.
func TestCheckoutShaMismatchExitsInfrastructure(t *testing.T) {
	repo := newFixtureRepo(t)
	dest := t.TempDir()
	if _, stderr, code := runCheckout(t, dest, "file://"+repo.dir, "main", repo.first); code != checkoutOK {
		t.Fatalf("seed checkout exit = %d\n%s", code, stderr)
	}

	_, stderr, code := runCheckout(t, dest, "file://"+repo.dir, "main", repo.second)
	if code != checkoutInfra {
		t.Fatalf("exit = %d, want %d\n%s", code, checkoutInfra, stderr)
	}
	if !strings.Contains(stderr, repo.first) || !strings.Contains(stderr, repo.second) {
		t.Errorf("stderr should name both the actual and pinned SHA:\n%s", stderr)
	}
	if got := headOf(t, filepath.Join(dest, "fixture")); got != repo.first {
		t.Errorf("script moved HEAD of an existing checkout to %s", got)
	}
}

func TestCheckoutUnfetchableCommitExitsInfrastructure(t *testing.T) {
	repo := newFixtureRepo(t)
	missing := strings.Repeat("ab", 20)

	_, stderr, code := runCheckout(t, t.TempDir(), "file://"+repo.dir, "main", missing)
	if code != checkoutInfra {
		t.Fatalf("exit = %d, want %d\n%s", code, checkoutInfra, stderr)
	}
}

func TestCheckoutUnreachableRepoExitsInfrastructure(t *testing.T) {
	missing := "file://" + filepath.Join(t.TempDir(), "no-such-repo")
	_, stderr, code := runCheckout(t, t.TempDir(), missing, "main", strings.Repeat("ab", 20))
	if code != checkoutInfra {
		t.Fatalf("exit = %d, want %d\n%s", code, checkoutInfra, stderr)
	}
}

// With no commit pinned the script resolves the ref and prints it for a human
// to paste into the manifest. It must not clone: a pin nobody reviewed is not
// something to run.
func TestCheckoutNullCommitPrintsResolvedShaWithoutCloning(t *testing.T) {
	repo := newFixtureRepo(t)
	dest := t.TempDir()

	// v1 is an annotated tag: the printed SHA must be the commit it points at,
	// not the tag object's own id.
	stdout, stderr, code := runCheckout(t, dest, "file://"+repo.dir, "v1", "")
	if code != checkoutUnpinned {
		t.Fatalf("exit = %d, want %d\n%s", code, checkoutUnpinned, stderr)
	}
	if stdout != repo.first {
		t.Errorf("stdout = %q, want the tag's commit %q", stdout, repo.first)
	}
	if _, err := os.Stat(filepath.Join(dest, "fixture")); !os.IsNotExist(err) {
		t.Errorf("an unpinned project was cloned anyway (stat err = %v)", err)
	}

	stdout, _, code = runCheckout(t, dest, "file://"+repo.dir, "main", "")
	if code != checkoutUnpinned || stdout != repo.second {
		t.Errorf("branch ref: exit %d stdout %q, want %d and %q", code, stdout, checkoutUnpinned, repo.second)
	}
}

func TestCheckoutNullCommitUnknownRefExitsInfrastructure(t *testing.T) {
	repo := newFixtureRepo(t)
	_, stderr, code := runCheckout(t, t.TempDir(), "file://"+repo.dir, "no-such-ref", "")
	if code != checkoutInfra {
		t.Fatalf("exit = %d, want %d\n%s", code, checkoutInfra, stderr)
	}
}

func TestCheckoutUsageErrors(t *testing.T) {
	repo := newFixtureRepo(t)
	pin := strings.Repeat("ab", 20)
	cases := map[string][]string{
		"unknown id":         {"nope"},
		"path-shaped id":     {"../escape"},
		"too many arguments": {"fixture", "extra"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, stderr, code := runCheckout(t, t.TempDir(), "file://"+repo.dir, "main", pin, args...)
			if code != checkoutUsage {
				t.Errorf("exit = %d, want %d\n%s", code, checkoutUsage, stderr)
			}
		})
	}
	t.Run("malformed commit", func(t *testing.T) {
		_, stderr, code := runCheckout(t, t.TempDir(), "file://"+repo.dir, "main", "deadbeef")
		if code != checkoutUsage {
			t.Errorf("exit = %d, want %d (a short SHA is a manifest bug, not infrastructure)\n%s", code, checkoutUsage, stderr)
		}
	})
}
