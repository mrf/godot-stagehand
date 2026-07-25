# Release Checklist

Use this checklist to ensure consistent and high-quality releases.

## Asset Naming Convention

Published assets use **version-less bare binary names**: `godot-stagehand-{platform}-{arch}[.exe]`

| Asset | Type | URL suffix |
|-------|------|------------|
| `godot-stagehand-linux-amd64` | bare binary | `releases/latest/download/godot-stagehand-linux-amd64` |
| `godot-stagehand-darwin-amd64` | bare binary | `releases/latest/download/godot-stagehand-darwin-amd64` |
| `godot-stagehand-darwin-arm64` | bare binary | `releases/latest/download/godot-stagehand-darwin-arm64` |
| `godot-stagehand-windows-amd64.exe` | bare binary | `releases/latest/download/godot-stagehand-windows-amd64.exe` |

`build-release.sh` outputs these names directly into `build/`. The `release.yml`
workflow publishes the same bare files, then downloads and executes each one on
its matching GitHub-hosted runner.

## Pre-Build Checks
- [ ] Bump the version with `./scripts/set-version.sh <version>` — it rewrites the
      authoritative constant in `internal/version/version.go` and every mirror
      (`plugin.cfg`, `stagehand_version.gd`, all addon copies). Never hand-edit
      a single file; see `docs/versioning.md`.
- [ ] Update version in documentation if needed
- [ ] Ensure all tests pass: `go test ./...`
- [ ] Check syntax errors in GDScript files
- [ ] Verify all necessary files are included for distribution

## Build & Test Phase
- [ ] Build Go binary for major platforms:
  - [ ] Linux: `GOOS=linux GOARCH=amd64 go build -o godot-stagehand-linux-amd64 .`
  - [ ] macOS Intel: `GOOS=darwin GOARCH=amd64 go build -o godot-stagehand-darwin-amd64 .`
  - [ ] macOS Apple Silicon: `GOOS=darwin GOARCH=arm64 go build -o godot-stagehand-darwin-arm64 .`
  - [ ] Windows: `GOOS=windows GOARCH=amd64 go build -o godot-stagehand-windows-amd64.exe .`
  - [ ] Ensure naming scheme follows convention: `godot-stagehand-{platform}-{arch}`
- [ ] Test the binary works properly on each platform
- [ ] Run integration tests if available
- [ ] Smoke test: Complete the quickstart guide end-to-end

## Documentation Verification
- [ ] Quickstart in README.md works in clean environment
- [ ] Examples/ directory updated to use correct stagehand version
- [ ] Copy addon script works correctly
- [ ] Version numbers are consistent across all files (`go test ./internal/version/`)
- [ ] `godot-stagehand --version` reports the tag version and the `gwp/N` protocol
- [ ] Changelog or release notes updated

## Release Artifacts Preparation
- [ ] Confirm the four bare binaries match the asset matrix above
- [ ] Confirm the published-asset smoke matrix passes on all four runners
- [ ] Include a summary of features/changes in the release
- [ ] Verify examples/ directory includes working example project
- [ ] Test the example project actually works with Stagehand connection

## Final Quality Assurance
- [ ] Verify the addon can be successfully copied to another Godot project using the script
- [ ] Confirm Godot connects and runs without errors
- [ ] Test core automation commands function properly
- [ ] Cleanup build artifacts not meant for distribution

## Version Control & Tagging
- [ ] Commit all changes
- [ ] Tag the release: `git tag v0.1.0` (with appropriate version)
- [ ] Push tags: `git push origin --tags`
- [ ] Prepare release notes for GitHub/GitLab releases page
