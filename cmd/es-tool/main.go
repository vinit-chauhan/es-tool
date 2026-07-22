// Command es-tool is a tiny CLI/REPL/TUI to view and edit Elasticsearch state.
package main

import (
	"os"

	"github.com/vinit-chauhan/es-tool/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Main(version, os.Args[1:]))
}
