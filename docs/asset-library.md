# Godot Asset Library packaging

The Asset Library entry points at `github.com/mrf/godot-stagehand` and a commit
SHA. The Godot editor downloads `https://github.com/mrf/godot-stagehand/archive/<sha>.zip`
and unpacks it into the user's project.

Without `.gitattributes` that zip is the **entire repo** — Go server, `testdata/`
(3.4 MB), `docs/`, `examples/`, `.beads/`, CI config — roughly 6 MB of which the
addon is 244 KB. The v0.3.0 submission was rejected for exactly this:

> You will want to add a .gitattributes file so that users download just your
> asset instead of the whole repo as it is pretty excessive.

## The fix

`.gitattributes` uses the pattern from the [submission guidelines](https://docs.godotengine.org/en/latest/community/asset_library/submitting_to_assetlib.html#recommendations):

```
/**        export-ignore
/addons    !export-ignore
/addons/** !export-ignore
```

`git archive` honours `export-ignore`, so the download contains only
`addons/stagehand/**`. `addons/stagehand/LICENSE` is a copy of the repo LICENSE
so the shipped addon is not unlicensed; keep it in sync (it is embedded into the
release binary by `//go:embed all:addons/stagehand` and synced to the vendored
copies by `scripts/sync-addon-copies.sh`).

## Known consequence: `go install` is broken

Go builds module zips with the same `git archive` call
(`$GOROOT/src/cmd/go/internal/modfetch/codehost/git.go`, `ReadZip`), so it also
honours `export-ignore`. A module zip cut from a tag made after this landed
contains no `go.mod` and no `.go` files, and

```
go install github.com/mrf/godot-stagehand@<rev>
```

fails. Versions already cached by `proxy.golang.org` before this change keep
resolving; new tags will not.

This was accepted deliberately. The README's install path is the prebuilt
release binary, and the Godot editor's own setup wizard downloads the same
assets (`addons/stagehand/editor/release_assets.gd`) — neither goes through
`go install`. Building from a clone (`go build -o godot-stagehand .`) is
unaffected, because a working tree is not an archive.

**If you ever need `go install` back**, the only way is to narrow the
`export-ignore` set to keep `go.mod`, `go.sum`, `*.go` and `internal/` — which
means Asset Library users get Go source dropped into their project root, the
thing the reviewer objected to. Pick one; you cannot have both from a single
tree.
