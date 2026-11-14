package main

import (
	"os"
	"strings"
	"testing"
)

// The community files are the public front door: an external reporter lands on
// them before anything else. These tests pin the contract the 87s.23 ticket set
// (issue forms collect the fields a bug report needs, the README points at the
// contributor docs, the triage path is written down) without parsing YAML —
// the repo has no YAML dependency, and CI validates the forms' syntax with a
// real parser.

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s must exist: %v", path, err)
	}
	return string(blob)
}

func requireContains(t *testing.T, path, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s must mention %q", path, want)
		}
	}
}

func TestBugReportFormCollectsDiagnosticFields(t *testing.T) {
	const path = ".github/ISSUE_TEMPLATE/bug_report.yml"
	body := readRepoFile(t, path)
	requireContains(t, path, body,
		"name:", "description:", "body:", "type: textarea", "required: true",
		"Godot version", "godot-stagehand version", "WSL",
		"MCP client", "CLI", "Scenario runner", "log",
	)
}

func TestFeatureRequestFormExists(t *testing.T) {
	const path = ".github/ISSUE_TEMPLATE/feature_request.yml"
	body := readRepoFile(t, path)
	requireContains(t, path, body, "name:", "description:", "body:", "type: textarea")
}

func TestIssueTemplateConfigRoutesQuestionsElsewhere(t *testing.T) {
	const path = ".github/ISSUE_TEMPLATE/config.yml"
	body := readRepoFile(t, path)
	requireContains(t, path, body, "contact_links:", "url:", "docs/troubleshooting.md", "SECURITY.md")
}

func TestContributingCoversTheBar(t *testing.T) {
	const path = "CONTRIBUTING.md"
	body := readRepoFile(t, path)
	requireContains(t, path, body,
		"go vet ./...", "go test ./...", "docs/addon-sync-contract.md",
		"addons/stagehand", "SECURITY.md",
	)
}

func TestSecurityPolicyPointsAtPrivateReporting(t *testing.T) {
	const path = "SECURITY.md"
	body := readRepoFile(t, path)
	requireContains(t, path, body, "docs/security.md", "privately", "advisories/new")
}

func TestREADMEPointsAtContributorDocs(t *testing.T) {
	const path = "README.md"
	body := readRepoFile(t, path)
	requireContains(t, path, body, "(CONTRIBUTING.md)", "(SECURITY.md)")
}

func TestTriagePathIsDocumented(t *testing.T) {
	const path = "docs/issue-triage.md"
	body := readRepoFile(t, path)
	requireContains(t, path, body, "--external-ref", "br", "GitHub")
	requireContains(t, "CLAUDE.md", readRepoFile(t, "CLAUDE.md"), "docs/issue-triage.md")
}
