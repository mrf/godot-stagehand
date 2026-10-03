// Nothing in this directory is Go. This file only fences tools/claude-mod off
// from the repository module, because `npm ci` installs node_modules here and
// some npm packages ship Go sources that `go test ./...` and `go vet ./...`
// would otherwise pick up.
module github.com/mrf/godot-stagehand/tools/claude-mod

go 1.25.5
