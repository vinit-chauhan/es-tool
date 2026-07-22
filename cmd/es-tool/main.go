// Command es-tool is a tiny CLI/REPL/TUI to view and edit Elasticsearch state.
package main

import (
	"os"

	"github.com/vinit-chauhan/es-tool/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
