# Release Checklist

Use this checklist to ensure consistent and high-quality releases.

## Pre-Build Checks
- [ ] Update version in `plugin.cfg` (in `addons/stagehand/plugin.cfg`)
- [ ] Update version in documentation if needed
- [ ] Ensure all tests pass: `go test ./...`
- [ ] Check syntax errors in GDScript files
- [ ] Verify all necessary files are included for distribution

## Build & Test Phase
- [ ] Build Go binary for major platforms:
  - [ ] Linux: `GOOS=linux GOARCH=amd64 go build -o godot-stagehand-linux-amd64 .`
  - [ ] macOS: `GOOS=darwin GOARCH=amd64 go build -o godot-stagehand-darwin-amd64 .`
  - [ ] Windows: `GOOS=windows GOARCH=amd64 go build -o godot-stagehand-windows-amd64.exe .`
  - [ ] Ensure naming scheme follows convention: `godot-stagehand-{platform}-{arch}`
- [ ] Test the binary works properly on each platform
- [ ] Run integration tests if available
- [ ] Smoke test: Complete the quickstart guide end-to-end

## Documentation Verification
- [ ] Quickstart in README.md works in clean environment
- [ ] Examples/ directory updated to use correct stagehand version
- [ ] Copy addon script works correctly
- [ ] Version numbers are consistent across all files
- [ ] Changelog or release notes updated

## Release Artifacts Preparation
- [ ] Create a zip archive containing binaries for each platform
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