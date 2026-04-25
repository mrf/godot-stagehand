package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// TODO: CLI flags, MCP server startup
	fmt.Println("godot-stagehand MCP server")
	return nil
}
