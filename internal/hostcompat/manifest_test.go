package hostcompat

import (
	"strings"
	"testing"
)

// baseProject returns a fully-valid, enabled project entry. Tests mutate one
// field at a time so a failure names exactly the rule that fired.
func baseProject() Project {
	return Project{
		ID:           "example",
		Name:         "Example",
		Repo:         "https://github.com/example/example",
		Ref:          "v1.0.0",
		Commit:       "0123456789abcdef0123456789abcdef01234567",
		GodotVersion: "4.6.3",
		License:      "MIT",
		Axes:         []string{AxisOwnCLIParser},
		Scenario:     "example/scenario.json",
		Enabled:      true,
		Claims: map[string]Claim{
			ClaimLanguage:     {Status: StatusVerified, Source: "checked project.godot"},
			ClaimGodotVersion: {Status: StatusVerified, Source: "checked project.godot"},
			ClaimLicense:      {Status: StatusVerified, Source: "checked LICENSE"},
			ClaimAxes:         {Status: StatusVerified, Source: "checked project.godot"},
		},
	}
}

func manifestOf(projects ...Project) *Manifest {
	return &Manifest{SchemaVersion: SchemaVersion, Projects: projects}
}

func mutate(f func(*Project)) *Manifest {
	p := baseProject()
	f(&p)
	return manifestOf(p)
}

func TestValidateAcceptsAWellFormedManifest(t *testing.T) {
	if err := manifestOf(baseProject()).Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestValidateRejectsBadEntries(t *testing.T) {
	tests := []struct {
		name   string
		m      *Manifest
		errHas string
	}{
		{
			"unsupported schema version",
			&Manifest{SchemaVersion: 99, Projects: []Project{baseProject()}},
			"schema_version",
		},
		{"no projects", manifestOf(), "at least one project"},
		{"empty id", mutate(func(p *Project) { p.ID = "" }), "id"},
		{"id charset", mutate(func(p *Project) { p.ID = "Not_An_ID" }), "id"},
		{"missing name", mutate(func(p *Project) { p.Name = "" }), "name"},
		{"missing repo", mutate(func(p *Project) { p.Repo = "" }), "repo"},
		{
			"non-https repo",
			mutate(func(p *Project) { p.Repo = "git@github.com:example/example.git" }),
			"repo",
		},
		{"missing ref", mutate(func(p *Project) { p.Ref = "" }), "ref"},
		{
			"short commit",
			mutate(func(p *Project) { p.Commit = "0123456" }),
			"commit",
		},
		{
			"uppercase commit",
			mutate(func(p *Project) { p.Commit = strings.ToUpper(baseProject().Commit) }),
			"commit",
		},
		{
			"godot 3.x",
			mutate(func(p *Project) { p.GodotVersion = "3.5.2" }),
			"godot_version",
		},
		{
			"godot version not a version",
			mutate(func(p *Project) { p.GodotVersion = "latest" }),
			"godot_version",
		},
		{"missing license", mutate(func(p *Project) { p.License = "" }), "license"},
		{"no axes", mutate(func(p *Project) { p.Axes = nil }), "axes"},
		{
			"unknown axis",
			mutate(func(p *Project) { p.Axes = []string{"popularity"} }),
			"unknown stress axis",
		},
		{
			"duplicate axis",
			mutate(func(p *Project) { p.Axes = []string{AxisOwnCLIParser, AxisOwnCLIParser} }),
			"duplicate",
		},
		{
			"scenario escapes the fixture dir",
			mutate(func(p *Project) { p.Scenario = "../../../etc/passwd" }),
			"scenario",
		},
		{
			"absolute scenario path",
			mutate(func(p *Project) { p.Scenario = "/tmp/scenario.json" }),
			"scenario",
		},
		{
			"missing claim",
			mutate(func(p *Project) { delete(p.Claims, ClaimLicense) }),
			ClaimLicense,
		},
		{
			"unknown claim",
			mutate(func(p *Project) {
				p.Claims["vibes"] = Claim{Status: StatusVerified, Source: "trust me"}
			}),
			"unknown claim",
		},
		{
			"bad claim status",
			mutate(func(p *Project) {
				p.Claims[ClaimLicense] = Claim{Status: "probably", Source: "x"}
			}),
			"status",
		},
		{
			"verified claim without a source",
			mutate(func(p *Project) {
				p.Claims[ClaimLicense] = Claim{Status: StatusVerified}
			}),
			"source",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got nil", tc.errHas)
			}
			if !strings.Contains(err.Error(), tc.errHas) {
				t.Errorf("error %q does not mention %q", err, tc.errHas)
			}
		})
	}
}

func TestValidateRejectsDuplicateIDs(t *testing.T) {
	a := baseProject()
	b := baseProject()
	b.Name = "Other"
	err := manifestOf(a, b).Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate project id") {
		t.Fatalf("expected a duplicate-id error, got %v", err)
	}
}

// The enable gate is the whole point of the claims block: a project may not run
// in CI until every claim about it has been checked against a real source and
// its pin has been resolved to an immutable SHA. An unresolved pin means we
// would be tracking a movable tag.
func TestEnabledRequiresResolvedPinAndVerifiedClaims(t *testing.T) {
	tests := []struct {
		name   string
		m      *Manifest
		errHas string
	}{
		{
			"enabled without a resolved commit",
			mutate(func(p *Project) { p.Commit = "" }),
			"commit",
		},
		{
			"enabled with an unverified claim",
			mutate(func(p *Project) {
				p.Claims[ClaimGodotVersion] = Claim{Status: StatusUnverified}
			}),
			ClaimGodotVersion,
		},
		{
			"enabled without a scenario fixture",
			mutate(func(p *Project) { p.Scenario = "" }),
			"scenario",
		},
		{
			"enabled against a branch ref",
			mutate(func(p *Project) { p.Ref = "main" }),
			"branch",
		},
		{
			"branch refs are matched case-insensitively",
			mutate(func(p *Project) { p.Ref = "HEAD" }),
			"branch",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.errHas) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.errHas, err)
			}
		})
	}
}

// The same gaps are legal while a project is still a candidate: the manifest is
// meant to carry unresolved candidates so the work of resolving them is visible
// and reviewable rather than living in someone's head.
func TestDisabledProjectsMayCarryUnresolvedFields(t *testing.T) {
	m := mutate(func(p *Project) {
		p.Enabled = false
		p.Commit = ""
		p.Scenario = ""
		p.Claims[ClaimGodotVersion] = Claim{Status: StatusUnverified}
		p.Claims[ClaimAxes] = Claim{Status: StatusUnverified}
	})
	if err := m.Validate(); err != nil {
		t.Fatalf("candidate entry rejected: %v", err)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`{"schema_version":1,"projekts":[]}`))
	if err == nil {
		t.Fatal("expected a parse error for an unknown field")
	}
}

func TestAxisCoverageAndGaps(t *testing.T) {
	a := baseProject()
	a.ID = "a"
	a.Axes = []string{AxisOwnCLIParser, AxisHeavyAutoloads}
	b := baseProject()
	b.ID = "b"
	b.Axes = []string{AxisContentScale}

	m := manifestOf(a, b)
	coverage := m.AxisCoverage()
	if got := coverage[AxisOwnCLIParser]; len(got) != 1 || got[0] != "a" {
		t.Errorf("own-cli-parser coverage = %v, want [a]", got)
	}
	if got := coverage[AxisContentScale]; len(got) != 1 || got[0] != "b" {
		t.Errorf("content-scale coverage = %v, want [b]", got)
	}

	missing := m.MissingAxes()
	if len(missing) == 0 {
		t.Fatal("expected uncovered axes for a two-project manifest")
	}
	for _, axis := range missing {
		if len(coverage[axis]) != 0 {
			t.Errorf("axis %q reported missing but has coverage %v", axis, coverage[axis])
		}
	}
	// Coverage must count candidates too: an axis a disabled project covers is
	// still a planned axis, not a gap in the design.
	c := baseProject()
	c.ID = "c"
	c.Enabled = false
	c.Commit = ""
	c.Scenario = ""
	c.Axes = []string{AxisEmbeddedSubwindows}
	if got := manifestOf(a, b, c).AxisCoverage()[AxisEmbeddedSubwindows]; len(got) != 1 {
		t.Errorf("disabled project excluded from coverage: %v", got)
	}
}

func TestGodotMinors(t *testing.T) {
	a := baseProject()
	a.ID = "a"
	a.GodotVersion = "4.6.3"
	b := baseProject()
	b.ID = "b"
	b.GodotVersion = "4.6.1"
	c := baseProject()
	c.ID = "c"
	c.GodotVersion = "4.3"

	got := manifestOf(a, b, c).GodotMinors()
	want := []string{"4.3", "4.6"}
	if len(got) != len(want) {
		t.Fatalf("GodotMinors() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GodotMinors() = %v, want %v", got, want)
		}
	}
}

func TestEnabledFiltersCandidates(t *testing.T) {
	on := baseProject()
	on.ID = "on"
	off := baseProject()
	off.ID = "off"
	off.Enabled = false

	got := manifestOf(on, off).EnabledProjects()
	if len(got) != 1 || got[0].ID != "on" {
		t.Fatalf("EnabledProjects() = %v, want just [on]", got)
	}
}
