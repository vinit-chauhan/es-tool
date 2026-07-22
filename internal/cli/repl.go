package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

const replHelp = `Commands (run '<command> --help' for details):
  ping
  indices [pattern]
  mapping <index>
  count   <index> [--q LUCENE]
  search  <index> [--q LUCENE] [--size N] [--sort F:dir] [--source CSV] [--body JSON|@file] [--ids-only]
  get     <index> <id>
  index   <index> [<id>] --body JSON|@file [--refresh]
  update  <index> <id> [--set k=v ...] [--doc JSON|@file] [--upsert] [--refresh]
  edit    <index> <id> [--refresh]
  delete  <index> <id> [--yes] [--refresh]
  delete-by-query <index> [--q LUCENE] [--body JSON|@file] [--yes] [--refresh]

Meta:
  use <url>          # switch ES_URL for this session
  whoami             # show current connection
  help               # this message
  quit / exit        # leave the REPL`

// RunREPL starts the interactive line-oriented REPL.
func RunREPL(client *esclient.Client) {
	fmt.Printf("es-tool REPL — connected to %s\n", client.BaseURL)
	fmt.Print("type 'help' for commands, 'quit' to exit\n\n")

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("es> ")
		raw, err := reader.ReadString('\n')
		if err != nil { // EOF / Ctrl-D
			fmt.Println()
			return
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case line == "quit" || line == "exit":
			return
		case line == "help":
			fmt.Println(replHelp)
			continue
		case line == "whoami":
			fmt.Printf("url   : %s\n", client.BaseURL)
			fmt.Printf("auth  : %s\n", client.AuthMode())
			continue
		case strings.HasPrefix(line, "use "):
			client.BaseURL = strings.TrimRight(strings.TrimSpace(line[4:]), "/")
			fmt.Printf("now using %s\n", client.BaseURL)
			continue
		}

		tokens, err := util.ShellSplitErr(line)
		if err != nil {
			errMsg(err.Error())
			continue
		}
		if len(tokens) == 0 {
			continue
		}
		if err := dispatch(client, tokens); err != nil {
			if !errors.Is(err, errHandled) {
				errMsg(err.Error())
			}
		}
	}
}
