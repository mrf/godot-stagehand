package main

import "embed"

// addonAssets embeds the Stagehand Godot addon so the "setup" subcommand can
// install it into a target project without any external files. The "all:"
// prefix ensures dotfiles and underscore-prefixed files are included.
//
//go:embed all:addons/stagehand
var addonAssets embed.FS
