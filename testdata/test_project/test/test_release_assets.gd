@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Contract tests for the exact published release asset matrix, the pinned
## release tag, SHA256SUMS verification, and the verify-then-rename install.

const ReleaseAssets := preload("res://addons/stagehand/editor/release_assets.gd")
const StagehandVersion := preload("res://addons/stagehand/stagehand_version.gd")

const LINUX_ASSET: String = "godot-stagehand-linux-amd64"
## sha256("hello")
const HELLO_SHA256: String = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"


func test_supported_assets_have_exact_names_and_pinned_urls() -> void:
	_assert_asset("Linux", "x86_64", LINUX_ASSET)
	_assert_asset("macOS", "x86_64", "godot-stagehand-darwin-amd64")
	_assert_asset("macOS", "arm64", "godot-stagehand-darwin-arm64")
	_assert_asset("Windows", "x86_64", "godot-stagehand-windows-amd64.exe")


func test_unpublished_platform_architectures_are_rejected() -> void:
	assert_str(ReleaseAssets.asset_name("Linux", "arm64")).is_empty()
	assert_str(ReleaseAssets.asset_name("Linux", "x86_32")).is_empty()
	assert_str(ReleaseAssets.asset_name("Windows", "arm64")).is_empty()


func test_release_tag_is_pinned_to_addon_version() -> void:
	assert_str(ReleaseAssets.RELEASE_TAG).is_equal("v%s" % StagehandVersion.VERSION)
	assert_str(ReleaseAssets.current_download_url()).contains(
		"/releases/download/v%s/" % StagehandVersion.VERSION
	)
	assert_str(ReleaseAssets.current_download_url()).not_contains("latest")


func test_checksums_url_uses_the_same_tag() -> void:
	assert_str(ReleaseAssets.checksums_url()).is_equal(
		"https://github.com/mrf/godot-stagehand/releases/download/v%s/SHA256SUMS"
		% StagehandVersion.VERSION
	)


func test_checksum_error_accepts_matching_buffer() -> void:
	var sums: String = _sums_with(HELLO_SHA256)
	assert_str(ReleaseAssets.sha256_hex("hello".to_utf8_buffer())).is_equal(HELLO_SHA256)
	assert_str(ReleaseAssets.checksum_error(
		ReleaseAssets.sha256_hex("hello".to_utf8_buffer()), sums, LINUX_ASSET
	)).is_empty()


func test_checksum_error_rejects_mismatched_buffer() -> void:
	var sums: String = _sums_with(HELLO_SHA256)
	var actual: String = ReleaseAssets.sha256_hex("hellp".to_utf8_buffer())
	assert_str(ReleaseAssets.checksum_error(actual, sums, LINUX_ASSET)).contains("mismatch")


func test_checksum_error_rejects_missing_asset_line() -> void:
	# A line for a longer name sharing the prefix must not count as a match.
	var sums: String = "%s  %s.exe\n" % [HELLO_SHA256, LINUX_ASSET]
	assert_str(ReleaseAssets.expected_sha256(sums, LINUX_ASSET)).is_empty()
	assert_str(ReleaseAssets.checksum_error(HELLO_SHA256, sums, LINUX_ASSET)).contains(
		"no line"
	)


func test_checksum_error_rejects_empty_actual_digest() -> void:
	# FileAccess.get_sha256 returns "" on failure; that must never verify.
	var sums: String = "%s  %s\n" % ["", LINUX_ASSET]
	assert_str(ReleaseAssets.checksum_error("", sums, LINUX_ASSET)).is_not_empty()


func test_install_verified_renames_part_over_existing_binary() -> void:
	var dir: String = ProjectSettings.globalize_path(create_temp_dir("release_install_ok"))
	var dest: String = dir.path_join("godot-stagehand")
	_write(dest, "old binary")
	_write(dest + ReleaseAssets.PART_SUFFIX, "hello")

	var err: String = ReleaseAssets.install_verified(
		dest + ReleaseAssets.PART_SUFFIX, dest, _sums_with(HELLO_SHA256), LINUX_ASSET
	)

	assert_str(err).is_empty()
	assert_str(FileAccess.get_file_as_string(dest)).is_equal("hello")
	assert_bool(FileAccess.file_exists(dest + ReleaseAssets.PART_SUFFIX)).is_false()


func test_install_verified_mismatch_keeps_existing_binary() -> void:
	var dir: String = ProjectSettings.globalize_path(create_temp_dir("release_install_bad"))
	var dest: String = dir.path_join("godot-stagehand")
	_write(dest, "old binary")
	_write(dest + ReleaseAssets.PART_SUFFIX, "truncat")

	var err: String = ReleaseAssets.install_verified(
		dest + ReleaseAssets.PART_SUFFIX, dest, _sums_with(HELLO_SHA256), LINUX_ASSET
	)

	assert_str(err).contains("mismatch")
	assert_str(FileAccess.get_file_as_string(dest)).is_equal("old binary")
	assert_bool(FileAccess.file_exists(dest + ReleaseAssets.PART_SUFFIX)).is_false()


func test_default_binary_path_is_outside_the_project() -> void:
	var data_dir: String = "/home/u/.local/share"
	assert_str(ReleaseAssets.default_binary_path("Linux", data_dir)).is_equal(
		"/home/u/.local/share/godot-stagehand/godot-stagehand"
	)
	assert_str(ReleaseAssets.default_binary_path("Windows", "C:/Users/u/AppData/Roaming")).is_equal(
		"C:/Users/u/AppData/Roaming/godot-stagehand/godot-stagehand.exe"
	)


func _assert_asset(os_name: String, arch_name: String, expected_name: String) -> void:
	assert_str(ReleaseAssets.asset_name(os_name, arch_name)).is_equal(expected_name)
	assert_str(ReleaseAssets.download_url(os_name, arch_name)).is_equal(
		"https://github.com/mrf/godot-stagehand/releases/download/v%s/%s"
		% [StagehandVersion.VERSION, expected_name]
	)


func _sums_with(linux_digest: String) -> String:
	return "%s  godot-stagehand-darwin-amd64\n%s  %s\n" % [
		"0".repeat(64), linux_digest, LINUX_ASSET
	]


func _write(path: String, content: String) -> void:
	var file: FileAccess = FileAccess.open(path, FileAccess.WRITE)
	file.store_string(content)
	file.close()


@warning_ignore_restore("return_value_discarded")
