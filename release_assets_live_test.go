package main

import (
	"net/http"
	"os"
	"testing"
	"time"
)

// TestLiveReleaseAssetsResolve hits the real GitHub Releases redirect
// endpoint for every published asset name. It is opt-in (set
// STAGEHAND_LIVE_RELEASE_CHECK=1) so the default `go test ./...` run stays
// network-free; run it manually or from a release-verification job after
// cutting a release to catch a renamed/missing asset the offline contract
// tests (TestReleaseAssetSurfacesMatchFixture) can't see, since those only
// check that the matrix agrees with itself across files, not that GitHub
// actually has assets by these names.
func TestLiveReleaseAssetsResolve(t *testing.T) {
	if os.Getenv("STAGEHAND_LIVE_RELEASE_CHECK") == "" {
		t.Skip("STAGEHAND_LIVE_RELEASE_CHECK not set; skipping live GitHub Releases check")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	for _, assetName := range releaseAssetNames {
		url := "https://github.com/mrf/godot-stagehand/releases/latest/download/" + assetName
		resp, err := client.Head(url)
		if err != nil {
			t.Errorf("HEAD %s: %v", url, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("HEAD %s = %d, want 200", url, resp.StatusCode)
		}
	}
}
