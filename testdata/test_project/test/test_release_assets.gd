@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Contract tests for the exact published release asset matrix.

const ReleaseAssets := preload("res://addons/stagehand/editor/release_assets.gd")


func test_supported_assets_have_exact_names_and_latest_urls() -> void:
	_assert_asset("Linux", "x86_64", "godot-stagehand-linux-amd64")
	_assert_asset("macOS", "x86_64", "godot-stagehand-darwin-amd64")
	_assert_asset("macOS", "arm64", "godot-stagehand-darwin-arm64")
	_assert_asset("Windows", "x86_64", "godot-stagehand-windows-amd64.exe")


func test_unpublished_platform_architectures_are_rejected() -> void:
	assert_str(ReleaseAssets.asset_name("Linux", "arm64")).is_empty()
	assert_str(ReleaseAssets.asset_name("Linux", "x86_32")).is_empty()
	assert_str(ReleaseAssets.asset_name("Windows", "arm64")).is_empty()


func _assert_asset(os_name: String, arch_name: String, expected_name: String) -> void:
	assert_str(ReleaseAssets.asset_name(os_name, arch_name)).is_equal(expected_name)
	assert_str(ReleaseAssets.download_url(os_name, arch_name)).is_equal(
		"https://github.com/mrf/godot-stagehand/releases/latest/download/%s" % expected_name
	)


@warning_ignore_restore("return_value_discarded")
