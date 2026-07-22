@tool
class_name StagehandReleaseAssets
extends RefCounted
## Pure, editor-side helpers that map the host OS/architecture to the matching
## godot-stagehand server release asset name and GitHub download URL.
##
## The exact supported matrix is four versionless bare binaries. Keep these
## names in sync with build-release.sh, release.yml, RELEASE_CHECKLIST.md, and
## README.md.

const RELEASE_BASE: String = "https://github.com/mrf/godot-stagehand/releases"
## Sentinel tag meaning "newest published release" — uses GitHub's
## /releases/latest/download/<asset> redirect endpoint.
const TAG_LATEST: String = "latest"

const ASSET_NAMES: Dictionary[String, String] = {
	"Linux/x86_64": "godot-stagehand-linux-amd64",
	"Linux/amd64": "godot-stagehand-linux-amd64",
	"macOS/x86_64": "godot-stagehand-darwin-amd64",
	"macOS/amd64": "godot-stagehand-darwin-amd64",
	"macOS/arm64": "godot-stagehand-darwin-arm64",
	"macOS/aarch64": "godot-stagehand-darwin-arm64",
	"Windows/x86_64": "godot-stagehand-windows-amd64.exe",
	"Windows/amd64": "godot-stagehand-windows-amd64.exe",
}


## Returns the release asset file name for the given OS/arch, or "" when the
## combination is unsupported. os_name uses OS.get_name() values; arch_name uses
## Engine.get_architecture_name() values.
static func asset_name(os_name: String, arch_name: String) -> String:
	var platform_arch: String = "%s/%s" % [os_name, arch_name]
	if not ASSET_NAMES.has(platform_arch):
		return ""
	var name: String = ASSET_NAMES[platform_arch]
	return name


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
