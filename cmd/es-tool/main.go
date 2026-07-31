// Command es-tool launches the interactive Elasticsearch terminal UI.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/tui"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	flags := flag.NewFlagSet("es-tool", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	index := flags.String("index", "", "jump directly to an index")
	showVersion := flags.Bool("version", false, "print version and exit")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: es-tool [--index INDEX] [--version]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "es-tool: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := tui.Run(esclient.NewFromEnv(), *index); err != nil {
		fmt.Fprintf(os.Stderr, "es-tool: %v\n", err)
		os.Exit(1)
	}
}
