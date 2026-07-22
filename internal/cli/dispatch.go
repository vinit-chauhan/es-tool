// Package cli implements the es-tool command-line interface: subcommand
// dispatch, the individual commands, and the interactive REPL.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

// errHandled is a sentinel meaning the error has already been reported to the
// user (message + body printed); callers should abort without re-printing.
var errHandled = errors.New("handled")

// command describes a subcommand: a one-line help string and its handler.
type command struct {
	help string
	run  func(client *esclient.Client, args []string) error
}

// commands is the subcommand registry, shared by the one-shot CLI and the REPL.
var commands = map[string]command{
	"ping":            {"GET / (cluster info)", cmdPing},
	"indices":         {"list indices (optionally filtered by pattern)", cmdIndices},
	"mapping":         {"get an index's mapping", cmdMapping},
	"count":           {"count docs (optionally Lucene-filtered)", cmdCount},
	"search":          {"run _search (Lucene query or JSON body)", cmdSearch},
	"get":             {"fetch a document by id", cmdGet},
	"index":           {"index (create/replace) a document", cmdIndex},
	"update":          {"partial update (POST _update)", cmdUpdate},
	"edit":            {"fetch a doc, open in $EDITOR, write back with OCC", cmdEdit},
	"delete":          {"delete a document by id", cmdDelete},
	"delete-by-query": {"delete docs matching a query", cmdDeleteByQuery},
	"tui":             {"full-screen interactive browser (select/view/edit/delete docs)", cmdTUI},
}

// commandOrder controls how commands are listed in help output.
var commandOrder = []string{
	"ping", "indices", "mapping", "count", "search", "get",
	"index", "update", "edit", "delete", "delete-by-query", "repl", "tui", "version",
}

// Main is the process entrypoint used by cmd/es-tool. It returns the exit code.
func Main(version string, argv []string) int {
	if len(argv) == 0 || argv[0] == "-h" || argv[0] == "--help" || argv[0] == "help" {
		usage()
		if len(argv) == 0 {
			return 2
		}
		return 0
	}
	if argv[0] == "version" || argv[0] == "--version" || argv[0] == "-v" {
		fmt.Printf("es-tool %s\n", version)
		return 0
	}

	client := esclient.NewFromEnv()
	if err := dispatch(client, argv); err != nil {
		if !errors.Is(err, errHandled) {
			errMsg(err.Error())
		}
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, "es-tool — tiny CLI/REPL/TUI to view and edit Elasticsearch state.")
	fmt.Fprintln(os.Stderr, "\nusage: es-tool <command> [args]\n\ncommands:")
	for _, name := range commandOrder {
		var help string
		switch name {
		case "repl":
			help = "start interactive REPL"
		case "version":
			help = "print version and exit"
		default:
			if c, ok := commands[name]; ok {
				help = c.help
			}
		}
		fmt.Fprintf(os.Stderr, "  %-16s %s\n", name, help)
	}
	fmt.Fprintln(os.Stderr, "\nConfig via env: ES_URL, ES_API_KEY, ES_USER, ES_PASSWORD, ES_VERIFY_TLS")
}

// dispatch runs a single command line (subcommand + args). It is used by both
// the one-shot CLI and the REPL.
func dispatch(client *esclient.Client, args []string) error {
	if len(args) == 0 {
		return errors.New("no command given")
	}
	name := args[0]
	rest := args[1:]

	if name == "repl" {
		RunREPL(client)
		return nil
	}
	c, ok := commands[name]
	if !ok {
		return fmt.Errorf("unknown command: %s", name)
	}
	return c.run(client, rest)
}

// ---------------------------------------------------------------------------
// Output helpers
// ---------------------------------------------------------------------------

func printJSON(data any) { fmt.Println(util.Dump(data)) }

func errMsg(msg string) { fmt.Fprintf(os.Stderr, "error: %s\n", msg) }

// mustOK returns the body on 2xx, otherwise prints "error: HTTP N" + the body
// and returns errHandled.
func mustOK(status int, body any) (any, error) {
	if status >= 200 && status < 300 {
		return body, nil
	}
	errMsg(fmt.Sprintf("HTTP %d", status))
	printJSON(body)
	return nil, errHandled
}

// ---------------------------------------------------------------------------
// Flag parsing helpers
// ---------------------------------------------------------------------------

// reorder moves flag arguments (and their values) ahead of positional
// arguments so Go's flag package, which stops at the first non-flag token, can
// parse flags that appear after positionals (as argparse allows).
func reorder(args []string, boolFlags map[string]bool) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && strings.HasPrefix(a, "-") && a != "--" {
			name := strings.TrimLeft(a, "-")
			hasEq := strings.Contains(name, "=")
			if idx := strings.IndexByte(name, '='); idx >= 0 {
				name = name[:idx]
			}
			flags = append(flags, a)
			if !hasEq && !boolFlags[name] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}

// parseFlags parses args into fs, reordering so flags may follow positionals.
// On parse error the flag package has already printed usage, so errHandled is
// returned.
func parseFlags(fs *flag.FlagSet, args []string, boolFlags map[string]bool) error {
	if err := fs.Parse(reorder(args, boolFlags)); err != nil {
		return errHandled
	}
	return nil
}

func refreshParam(on bool) map[string]string {
	if on {
		return map[string]string{"refresh": "true"}
	}
	return nil
}

// confirm prompts on stdin and returns true for y/yes.
func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// hitsOf extracts resp["hits"]["hits"] as a slice.
func hitsOf(resp map[string]any) []any {
	hits, ok := resp["hits"].(map[string]any)
	if !ok {
		return nil
	}
	arr, _ := hits["hits"].([]any)
	return arr
}
