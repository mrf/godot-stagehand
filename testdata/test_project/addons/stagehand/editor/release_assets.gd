@tool
class_name StagehandReleaseAssets
extends RefCounted
## Pure, editor-side helpers that map the host OS/architecture to the matching
## godot-stagehand server release asset name and GitHub download URL.
##
## NOTE (vrj.12 gating): the GitHub Releases assets these URLs point at do not
## necessarily exist yet — release publishing is tracked separately by
## godot-stagehand-phase3-vrj.12. The URL pattern below is the single documented
## source of truth; if the published asset naming differs, change it HERE only.
##
## The naming convention follows RELEASE_CHECKLIST.md:
##   "Ensure naming scheme follows convention: godot-stagehand-{platform}-{arch}"
## i.e. a bare (un-archived) binary named:
##   godot-stagehand-<platform>-<arch>[.exe]
## with <platform>/<arch> using Go's GOOS/GOARCH tokens (linux/darwin/windows,
## amd64/arm64/386), which is what build-release.sh produces.
##
## KNOWN INCONSISTENCY: build-release.sh embeds the version in the file name
## (godot-stagehand-<version>-<platform>-<arch>) while RELEASE_CHECKLIST.md does
## not. We follow the version-less RELEASE_CHECKLIST convention because it is the
## only scheme compatible with the "releases/latest/download/<asset>" URL (you
## can't construct a versioned name without already knowing the version). This is
## flagged for the release work (vrj.12) to reconcile.

const BINARY_PREFIX: String = "godot-stagehand"
const REPO: String = "mrf/godot-stagehand"
const RELEASE_BASE: String = "https://github.com/mrf/godot-stagehand/releases"
## Sentinel tag meaning "newest published release" — uses GitHub's
## /releases/latest/download/<asset> redirect endpoint.
const TAG_LATEST: String = "latest"

## OS.get_name() -> Go GOOS token.
const PLATFORM_TOKENS: Dictionary[String, String] = {
	"Windows": "windows",
	"macOS": "darwin",
	"Linux": "linux",
}

## Engine.get_architecture_name() -> Go GOARCH token. Several spellings are
## accepted because the value has varied across Godot builds/platforms
## (e.g. "x86_64" vs "amd64", "arm64" vs "aarch64").
const ARCH_TOKENS: Dictionary[String, String] = {
	"x86_64": "amd64",
	"amd64": "amd64",
	"arm64": "arm64",
	"aarch64": "arm64",
	"x86_32": "386",
	"i686": "386",
	"x86": "386",
}


## Returns the release asset file name for the given OS/arch, or "" when the
## combination is unsupported. os_name uses OS.get_name() values; arch_name uses
## Engine.get_architecture_name() values.
static func asset_name(os_name: String, arch_name: String) -> String:
	if not PLATFORM_TOKENS.has(os_name):
		return ""
	if not ARCH_TOKENS.has(arch_name):
		return ""
	var platform: String = PLATFORM_TOKENS[os_name]
	var arch: String = ARCH_TOKENS[arch_name]
	var suffix: String = ".exe" if os_name == "Windows" else ""
	return "%s-%s-%s%s" % [BINARY_PREFIX, platform, arch, suffix]


## Returns the full GitHub Releases download URL for the given OS/arch, or ""
## when unsupported. With the default tag the "latest" redirect endpoint is used.
static func download_url(os_name: String, arch_name: String, tag: String = TAG_LATEST) -> String:
	var name: String = asset_name(os_name, arch_name)
	if name.is_empty():
		return ""
	if tag == TAG_LATEST:
		return "%s/latest/download/%s" % [RELEASE_BASE, name]
	return "%s/download/%s/%s" % [RELEASE_BASE, tag, name]


## Convenience: asset name for the machine this editor is running on.
static func current_asset_name() -> String:
	return asset_name(OS.get_name(), Engine.get_architecture_name())


## Convenience: download URL for the machine this editor is running on.
static func current_download_url(tag: String = TAG_LATEST) -> String:
	return download_url(OS.get_name(), Engine.get_architecture_name(), tag)


## Whether the host binary must be marked executable after download (unix only).
static func needs_executable_bit(os_name: String) -> bool:
	return os_name != "Windows"
