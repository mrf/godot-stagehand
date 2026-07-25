// Package hostcompat models the host-project compatibility manifest: the
// checked-in, pinned set of third-party Godot projects the compatibility suite
// drives, plus the rules that keep it honest.
//
// Two rules carry the design and are enforced here rather than by convention:
//
//   - A project may not run in CI until its pin resolves to an immutable commit
//     SHA and every claim made about it has been checked against a real source.
//     Candidates that fail either test stay in the manifest, disabled, so the
//     outstanding work is visible instead of forgotten.
//   - A host scenario may only assert things about Stagehand, never about the
//     host application. See surface.go.
//
// See docs/design/host-project-compat-suite.md.
package hostcompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// SchemaVersion is the manifest format this package understands. Bumping it is
// a breaking change to every consumer, so it is deliberately separate from the
// tool version.
const SchemaVersion = 1

// Stress axes. A project earns its slot by covering an axis our own fixture
// cannot, so the axis list — not popularity — is the selection criterion.
const (
	// AxisOwnCLIParser: the host reads its own argv and rejects flags it does
	// not recognise, so "--stagehand" cannot be the activation path.
	AxisOwnCLIParser = "own-cli-parser"
	// AxisEmbeddedSubwindows: the host shows Window nodes embedded in the main
	// viewport rather than as OS windows.
	AxisEmbeddedSubwindows = "embedded-subwindows"
	// AxisContentScale: project.godot sets a content scale factor or stretch
	// mode, so node rects and window coordinates disagree.
	AxisContentScale = "content-scale"
	// AxisCustomInput: the host implements _input or _unhandled_input rather
	// than relying on Control focus handling.
	AxisCustomInput = "custom-input"
	// AxisHeavyAutoloads: the host registers many autoloads, so ours lands in a
	// crowded and order-sensitive singleton list.
	AxisHeavyAutoloads = "heavy-autoloads"
	// AxisLargeProject: the host has enough assets that first import dominates
	// the run, which is a real cost the suite must budget for.
	AxisLargeProject = "large-project"
)

// knownAxes is the closed set. A typo'd axis would silently create a phantom
// gap in coverage reporting, so unknown axes are a validation error.
var knownAxes = []string{
	AxisOwnCLIParser,
	AxisEmbeddedSubwindows,
	AxisContentScale,
	AxisCustomInput,
	AxisHeavyAutoloads,
	AxisLargeProject,
}

// KnownAxes returns the closed stress-axis set in declaration order.
func KnownAxes() []string { return slices.Clone(knownAxes) }

// Claim keys. Each names a fact about a third-party project that we must not
// take on reputation.
const (
	// ClaimLanguage: the project is GDScript, not Mono/C# (which needs a
	// different engine build entirely).
	ClaimLanguage = "language"
	// ClaimGodotVersion: the engine version read from the project itself, not
	// from a README or a release note.
	ClaimGodotVersion = "godot_version"
	// ClaimLicense: the licence permits cloning it in our CI.
	ClaimLicense = "license"
	// ClaimAxes: the declared stress axes were confirmed in the project's own
	// files, not inferred from what the project appears to be.
	ClaimAxes = "axes"
)

var knownClaims = []string{ClaimLanguage, ClaimGodotVersion, ClaimLicense, ClaimAxes}

// Claim statuses.
const (
	StatusVerified   = "verified"
	StatusUnverified = "unverified"
)

// Claim records whether a fact was checked and where.
type Claim struct {
	// Status is StatusVerified or StatusUnverified.
	Status string `json:"status"`
	// Source names where the fact was checked — a file in the project, a
	// command that was run, a page that was read. Required when verified;
	// when unverified it should say what would settle it.
	Source string `json:"source,omitempty"`
}

// Verified reports whether the claim has been checked against a real source.
func (c Claim) Verified() bool { return c.Status == StatusVerified }

// Project is one pinned third-party Godot project.
type Project struct {
	// ID is the stable slug used for checkout directories, cache keys, CI job
	// names and scenario paths. Renaming it invalidates caches.
	ID string `json:"id"`
	// Name is the human-readable project name.
	Name string `json:"name"`
	// Repo is the clone URL. HTTPS only: CI has no deploy key and should not
	// need one for a public read.
	Repo string `json:"repo"`
	// Ref is the human-readable pin — a tag, usually. It is documentation and
	// a clone hint; Commit is the authority, because a tag can be moved.
	Ref string `json:"ref"`
	// Commit is the 40-hex SHA Ref resolved to. Empty means the pin has not
	// been resolved yet, which keeps the project a candidate.
	Commit string `json:"commit,omitempty"`
	// GodotVersion is the engine version to run this project with, as
	// understood by scripts/ci-install-godot.sh (e.g. "4.6.3").
	GodotVersion string `json:"godot_version"`
	// License is the SPDX-ish licence identifier of the host project.
	License string `json:"license"`
	// ProjectSubdir locates project.godot within the checkout. Empty means the
	// repository root.
	ProjectSubdir string `json:"project_subdir,omitempty"`
	// Axes are the stress axes this project is in the suite to cover.
	Axes []string `json:"axes"`
	// Scenario is the host-compat scenario file for this project, relative to
	// the manifest's directory. Empty means the fixture is not written yet.
	Scenario string `json:"scenario,omitempty"`
	// Enabled runs this project in CI. Gated on a resolved pin, a scenario and
	// fully-verified claims.
	Enabled bool `json:"enabled"`
	// Claims records what has been checked about this project and where.
	Claims map[string]Claim `json:"claims"`
	// Notes is free-form context: why it was chosen, what is still open.
	Notes string `json:"notes,omitempty"`
}

// Manifest is the whole pinned project set.
type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	Projects      []Project `json:"projects"`
}

// branchLikeRefs are refs that move. A candidate may name one — it just says
// "resolve a pin from here" — but an enabled project may not.
var branchLikeRefs = []string{"main", "master", "head", "develop", "dev", "trunk"}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	godot4xPattern = regexp.MustCompile(`^4\.(\d+)(\.\d+)?$`)
)

// Parse decodes and validates a manifest. Unknown fields are rejected so a
// misspelled key cannot silently disable a rule.
func Parse(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Load reads and validates a manifest file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Validate checks every structural and honesty rule. It reports the first
// failure, named by project id so the fix is obvious.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (want %d)", m.SchemaVersion, SchemaVersion)
	}
	if len(m.Projects) == 0 {
		return fmt.Errorf("manifest must list at least one project")
	}
	seen := make(map[string]bool, len(m.Projects))
	for i := range m.Projects {
		p := &m.Projects[i]
		if err := p.validate(); err != nil {
			label := p.ID
			if label == "" {
				label = fmt.Sprintf("project #%d", i+1)
			}
			return fmt.Errorf("%s: %w", label, err)
		}
		if seen[p.ID] {
			return fmt.Errorf("duplicate project id %q", p.ID)
		}
		seen[p.ID] = true
	}
	return nil
}

func (p *Project) validate() error {
	if !idPattern.MatchString(p.ID) {
		return fmt.Errorf("id %q must be lowercase [a-z0-9-] and start alphanumeric", p.ID)
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !strings.HasPrefix(p.Repo, "https://") {
		return fmt.Errorf("repo %q must be an https:// clone URL", p.Repo)
	}
	if strings.TrimSpace(p.Ref) == "" {
		return fmt.Errorf("ref is required (the tag or branch the pin was taken from)")
	}
	if p.Commit != "" && !commitPattern.MatchString(p.Commit) {
		return fmt.Errorf("commit %q must be a 40-character lowercase hex SHA", p.Commit)
	}
	if !godot4xPattern.MatchString(p.GodotVersion) {
		return fmt.Errorf("godot_version %q must be a Godot 4.x version such as 4.6.3", p.GodotVersion)
	}
	if strings.TrimSpace(p.License) == "" {
		return fmt.Errorf("license is required")
	}
	if err := validateAxes(p.Axes); err != nil {
		return err
	}
	if err := validateRelPath("project_subdir", p.ProjectSubdir); err != nil {
		return err
	}
	if err := validateRelPath("scenario", p.Scenario); err != nil {
		return err
	}
	if err := validateClaims(p.Claims); err != nil {
		return err
	}
	if p.Enabled {
		return p.validateEnableGate()
	}
	return nil
}

// validateEnableGate is the honesty rule with teeth: running a third-party
// project in CI requires an immutable pin, a fixture, and no unchecked claims.
func (p *Project) validateEnableGate() error {
	if p.Commit == "" {
		return fmt.Errorf("enabled projects need a resolved commit; a bare ref tracks a movable tag")
	}
	if p.Scenario == "" {
		return fmt.Errorf("enabled projects need a scenario fixture to run")
	}
	// The commit is what CI checks out, so a branch-shaped ref is not a
	// correctness bug — but it records the pin as having been taken from a
	// moving target, which makes the next bump unreviewable. A pin should
	// name the release it came from.
	if slices.Contains(branchLikeRefs, strings.ToLower(p.Ref)) {
		return fmt.Errorf("ref %q is a branch; pin an enabled project to a tag or release", p.Ref)
	}
	for _, key := range knownClaims {
		if !p.Claims[key].Verified() {
			return fmt.Errorf("cannot enable with an unverified %q claim", key)
		}
	}
	return nil
}

func validateAxes(axes []string) error {
	if len(axes) == 0 {
		return fmt.Errorf("axes must name at least one stress axis this project covers")
	}
	seen := make(map[string]bool, len(axes))
	for _, axis := range axes {
		if !slices.Contains(knownAxes, axis) {
			return fmt.Errorf("unknown stress axis %q (known: %s)", axis, strings.Join(knownAxes, ", "))
		}
		if seen[axis] {
			return fmt.Errorf("duplicate axis %q", axis)
		}
		seen[axis] = true
	}
	return nil
}

// validateRelPath keeps checked-in paths inside the fixture tree. An absolute
// or escaping path in a manifest is either a mistake or an attempt to make CI
// read something it should not.
func validateRelPath(field, path string) error {
	if path == "" {
		return nil
	}
	slashed := strings.ReplaceAll(path, `\`, "/")
	if strings.HasPrefix(slashed, "/") || strings.Contains(slashed, ":") {
		return fmt.Errorf("%s %q must be a relative path", field, path)
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return fmt.Errorf("%s %q must not escape its directory", field, path)
		}
	}
	return nil
}

func validateClaims(claims map[string]Claim) error {
	for key := range claims {
		if !slices.Contains(knownClaims, key) {
			return fmt.Errorf("unknown claim %q (known: %s)", key, strings.Join(knownClaims, ", "))
		}
	}
	for _, key := range knownClaims {
		claim, ok := claims[key]
		if !ok {
			return fmt.Errorf("missing claim %q", key)
		}
		switch claim.Status {
		case StatusVerified:
			if strings.TrimSpace(claim.Source) == "" {
				return fmt.Errorf("claim %q is verified but names no source", key)
			}
		case StatusUnverified:
		default:
			return fmt.Errorf("claim %q has status %q (want %q or %q)",
				key, claim.Status, StatusVerified, StatusUnverified)
		}
	}
	return nil
}

// EnabledProjects returns the projects CI should actually run.
func (m *Manifest) EnabledProjects() []Project {
	var out []Project
	for _, p := range m.Projects {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
}

// AxisCoverage maps each known axis to the project ids covering it, in manifest
// order. Candidates count: a disabled project covering an axis is planned work,
// not a hole in the design.
func (m *Manifest) AxisCoverage() map[string][]string {
	coverage := make(map[string][]string, len(knownAxes))
	for _, axis := range knownAxes {
		coverage[axis] = nil
	}
	for _, p := range m.Projects {
		for _, axis := range p.Axes {
			coverage[axis] = append(coverage[axis], p.ID)
		}
	}
	return coverage
}

// MissingAxes returns the known axes no project claims, in declaration order.
func (m *Manifest) MissingAxes() []string {
	coverage := m.AxisCoverage()
	var missing []string
	for _, axis := range knownAxes {
		if len(coverage[axis]) == 0 {
			missing = append(missing, axis)
		}
	}
	return missing
}

// GodotMinors returns the distinct Godot 4.x minor series the manifest spans,
// sorted. A suite that runs every project on the same engine build tests one
// engine, not the 4.x line.
func (m *Manifest) GodotMinors() []string {
	seen := map[int]bool{}
	var minors []int
	for _, p := range m.Projects {
		match := godot4xPattern.FindStringSubmatch(p.GodotVersion)
		if match == nil {
			continue
		}
		minor, err := strconv.Atoi(match[1])
		if err != nil || seen[minor] {
			continue
		}
		seen[minor] = true
		minors = append(minors, minor)
	}
	// Sort numerically: lexical order would put 4.10 before 4.3.
	slices.Sort(minors)
	out := make([]string, 0, len(minors))
	for _, minor := range minors {
		out = append(out, "4."+strconv.Itoa(minor))
	}
	return out
}
