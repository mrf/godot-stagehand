@tool
class_name StagehandReleaseAssets
extends RefCounted

## GitHub Release asset names for the Stagehand setup wizard.
## Convention: version-less bare binaries — godot-stagehand-{platform}-{arch}[.exe]
## macOS assets are zipped; Linux and Windows are bare binaries.
## Download base: https://github.com/mrf/godot-stagehand/releases/latest/download/

const GITHUB_OWNER: String = "mrf"
const GITHUB_REPO: String = "godot-stagehand"
const DOWNLOAD_BASE: String = "https://github.com/" + GITHUB_OWNER + "/" + GITHUB_REPO + "/releases/latest/download/"

const LINUX_AMD64: String   = "godot-stagehand-linux-amd64"
const DARWIN_AMD64: String  = "godot-stagehand-darwin-amd64.zip"
const DARWIN_ARM64: String  = "godot-stagehand-darwin-arm64.zip"
const WINDOWS_AMD64: String = "godot-stagehand-windows-amd64.exe"


static func get_asset_name(os_name: String, arch: String) -> String:
	if os_name == "Linux" and arch == "x86_64":
		return LINUX_AMD64
	if os_name == "macOS" and arch == "x86_64":
		return DARWIN_AMD64
	if os_name == "macOS" and arch == "arm64":
		return DARWIN_ARM64
	if os_name == "Windows" and arch == "x86_64":
		return WINDOWS_AMD64
	return ""
