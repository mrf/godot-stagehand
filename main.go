package main

import (
	"fmt"
	"os"

	"github.com/mrf/godot-stagehand/internal/mcpserver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	srv := mcpserver.New()
	return srv.Serve()
}
