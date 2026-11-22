@tool
class_name StagehandReleaseAssets
extends RefCounted
## Editor-side helpers that map the host OS/architecture to the matching
## godot-stagehand server release asset name and GitHub download URL, and
## verify a downloaded asset against the release's SHA256SUMS before installing.
##
## The exact supported matrix is four versionless bare binaries. Keep these
## names in sync with build-release.sh, release.yml, docs/release-checklist.md, and
## README.md.

const StagehandVersion := preload("res://addons/stagehand/stagehand_version.gd")

const RELEASE_BASE: String = "https://github.com/mrf/godot-stagehand/releases"
## The release matching this addon. Never "latest": that would pair the addon
## with whatever binary was published most recently.
const RELEASE_TAG: String = "v" + StagehandVersion.VERSION
const CHECKSUMS_ASSET: String = "SHA256SUMS"
## Downloads land at <dest>.part and are renamed over <dest> only once verified.
const PART_SUFFIX: String = ".part"

const ASSET_NAMES: Dictionary = {
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
## when unsupported.
static func download_url(os_name: String, arch_name: String, tag: String = RELEASE_TAG) -> String:
	var name: String = asset_name(os_name, arch_name)
	if name.is_empty():
		return ""
	return "%s/download/%s/%s" % [RELEASE_BASE, tag, name]


## URL of the release's SHA256SUMS manifest (GNU sha256sum text format).
static func checksums_url(tag: String = RELEASE_TAG) -> String:
	return "%s/download/%s/%s" % [RELEASE_BASE, tag, CHECKSUMS_ASSET]


## Convenience: asset name for the machine this editor is running on.
static func current_asset_name() -> String:
	return asset_name(OS.get_name(), Engine.get_architecture_name())


## Convenience: download URL for the machine this editor is running on.
static func current_download_url(tag: String = RELEASE_TAG) -> String:
	return download_url(OS.get_name(), Engine.get_architecture_name(), tag)


## Whether the host binary must be marked executable after download (unix only).
static func needs_executable_bit(os_name: String) -> bool:
	return os_name != "Windows"


## Default install location: the OS's global user data dir, outside res:// so
## the binary is never imported, exported, or committed with the project.
static func default_binary_path(os_name: String, data_dir: String) -> String:
	var file_name: String = "godot-stagehand"
	if os_name == "Windows":
		file_name += ".exe"
	return data_dir.path_join("godot-stagehand").path_join(file_name)


static func sha256_hex(data: PackedByteArray) -> String:
	var ctx: HashingContext = HashingContext.new()
	var start_err: Error = ctx.start(HashingContext.HASH_SHA256)
	if start_err != OK:
		return ""
	var update_err: Error = ctx.update(data)
	if update_err != OK:
		return ""
	return ctx.finish().hex_encode()


## Returns the lowercase digest SHA256SUMS lists for asset, or "" when no line
## names exactly that asset.
static func expected_sha256(sums_text: String, asset: String) -> String:
	for raw_line: String in sums_text.split("\n", false):
		var line: String = raw_line.strip_edges()
		var sep: int = line.find(" ")
		if sep <= 0:
			continue
		var name: String = line.substr(sep).strip_edges().trim_prefix("*")
		if name == asset:
			return line.substr(0, sep).to_lower()
	return ""


## Returns "" when actual_sha256 matches asset's SHA256SUMS line, otherwise a
## human-readable reason.
static func checksum_error(actual_sha256: String, sums_text: String, asset: String) -> String:
	var expected: String = expected_sha256(sums_text, asset)
	if expected.is_empty():
		return "SHA256SUMS has no line for %s" % asset
	if actual_sha256.is_empty():
		return "could not hash the downloaded file"
	if actual_sha256.to_lower() != expected:
		return "SHA-256 mismatch for %s (expected %s, got %s)" % [asset, expected, actual_sha256]
	return ""


## Verifies part_path against SHA256SUMS and renames it over dest_path. On any
## failure the part file is removed and dest_path is left untouched. Returns ""
## on success, otherwise the reason.
static func install_verified(
	part_path: String, dest_path: String, sums_text: String, asset: String
) -> String:
	var reason: String = checksum_error(FileAccess.get_sha256(part_path), sums_text, asset)
	if reason.is_empty():
		var rename_err: Error = DirAccess.rename_absolute(part_path, dest_path)
		if rename_err == OK:
			return ""
		reason = "could not move %s into place: %s" % [part_path, error_string(rename_err)]
	if FileAccess.file_exists(part_path):
		var _remove_err: Error = DirAccess.remove_absolute(part_path)
	return reason
