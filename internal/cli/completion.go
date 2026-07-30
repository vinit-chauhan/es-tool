package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
)

type completionOption struct {
	name        string
	description string
	takesValue  bool
	fileValue   bool
}

type completionCommand struct {
	name        string
	description string
	flagHelp    bool
	options     []completionOption
}

var completionCommands = []completionCommand{
	{name: "ping", description: "GET / (cluster info)"},
	{name: "indices", description: "list indices (optionally filtered by pattern)", flagHelp: true},
	{name: "mapping", description: "get an index mapping", flagHelp: true},
	{
		name: "count", description: "count docs (optionally Lucene-filtered)", flagHelp: true,
		options: []completionOption{
			{name: "q", description: "Lucene query string", takesValue: true},
		},
	},
	{
		name: "search", description: "run _search (Lucene query or JSON body)", flagHelp: true,
		options: []completionOption{
			{name: "q", description: "Lucene query string", takesValue: true},
			{name: "size", description: "number of hits", takesValue: true},
			{name: "sort", description: "sort, for example @timestamp:desc", takesValue: true},
			{name: "source", description: "comma-separated _source includes", takesValue: true},
			{name: "body", description: "JSON body or @file", takesValue: true, fileValue: true},
			{name: "ids-only", description: "print only hit IDs"},
		},
	},
	{name: "get", description: "fetch a document by ID", flagHelp: true},
	{
		name: "index", description: "index (create or replace) a document", flagHelp: true,
		options: []completionOption{
			{name: "body", description: "JSON body or @file", takesValue: true, fileValue: true},
			{name: "refresh", description: "refresh after write"},
		},
	},
	{
		name: "update", description: "partially update a document", flagHelp: true,
		options: []completionOption{
			{name: "set", description: "field assignment K=V (repeatable)", takesValue: true},
			{name: "doc", description: "JSON object or @file to merge", takesValue: true, fileValue: true},
			{name: "upsert", description: "create the document when missing"},
			{name: "refresh", description: "refresh after write"},
		},
	},
	{
		name: "edit", description: "edit a document in $EDITOR", flagHelp: true,
		options: []completionOption{
			{name: "refresh", description: "refresh after write"},
		},
	},
	{
		name: "delete", description: "delete a document", flagHelp: true,
		options: []completionOption{
			{name: "yes", description: "skip confirmation"},
			{name: "refresh", description: "refresh after write"},
		},
	},
	{
		name: "delete-by-query", description: "delete documents matching a query", flagHelp: true,
		options: []completionOption{
			{name: "q", description: "Lucene query string", takesValue: true},
			{name: "body", description: "JSON body or @file", takesValue: true, fileValue: true},
			{name: "yes", description: "skip confirmation"},
			{name: "refresh", description: "refresh after write"},
		},
	},
	{name: "repl", description: "start the interactive REPL"},
	{
		name: "tui", description: "start the full-screen interactive browser", flagHelp: true,
		options: []completionOption{
			{name: "index", description: "jump directly into an index", takesValue: true},
		},
	},
	{name: "completion", description: "generate a shell completion script", flagHelp: true},
	{name: "help", description: "show command help"},
	{name: "version", description: "print the version"},
}

func cmdCompletion(_ *esclient.Client, args []string) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(os.Stderr, "usage: es-tool completion <bash|zsh|fish>")
		return nil
	}
	if len(args) != 1 {
		return errors.New("completion: shell required (bash, zsh, or fish)")
	}
	script, err := completionScript(strings.ToLower(args[0]))
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashCompletion(), nil
	case "zsh":
		return zshCompletion(), nil
	case "fish":
		return fishCompletion(), nil
	default:
		return "", fmt.Errorf("completion: unsupported shell %q (choose bash, zsh, or fish)", shell)
	}
}

func bashCompletion() string {
	var b strings.Builder
	commandNames := make([]string, 0, len(completionCommands)+4)
	for _, command := range completionCommands {
		commandNames = append(commandNames, command.name)
	}
	commandNames = append(commandNames, "-h", "--help", "-v", "--version")

	b.WriteString(`# bash completion for es-tool
_es_tool_completion() {
    local cur prev command opts path prefix i
    local -a files
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    if (( COMP_CWORD == 1 )); then
        COMPREPLY=( $(compgen -W '`)
	b.WriteString(strings.Join(commandNames, " "))
	b.WriteString(`' -- "$cur") )
        return
    fi

    command="${COMP_WORDS[1]}"
    if [[ "$command" == "completion" && "$COMP_CWORD" -eq 2 ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=( $(compgen -W '-h --help' -- "$cur") )
        else
            COMPREPLY=( $(compgen -W 'bash zsh fish' -- "$cur") )
        fi
        return
    fi

`)
	b.WriteString("    case \"$command:$prev\" in\n")
	for _, command := range completionCommands {
		for _, option := range command.options {
			if !option.takesValue {
				continue
			}
			fmt.Fprintf(&b, "        %q)\n", command.name+":--"+option.name)
			if option.fileValue {
				b.WriteString(`            path="$cur"
            prefix=""
            if [[ "$path" == @* ]]; then
                prefix="@"
                path="${path#@}"
            fi
            files=( $(compgen -f -- "$path") )
            if [[ "$prefix" == "@" ]]; then
                for i in "${!files[@]}"; do
                    files[$i]="@${files[$i]}"
                done
            fi
            COMPREPLY=("${files[@]}")
`)
			}
			b.WriteString("            return\n            ;;\n")
		}
	}
	b.WriteString("    esac\n\n    case \"$command\" in\n")
	for _, command := range completionCommands {
		options := completionOptionNames(command)
		if len(options) == 0 {
			continue
		}
		fmt.Fprintf(&b, "        %s) opts=%s ;;\n", command.name, shellSingleQuote(strings.Join(options, " ")))
	}
	b.WriteString(`        *) return ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "$opts" -- "$cur") )
    fi
}
complete -F _es_tool_completion es-tool
`)
	return b.String()
}

func zshCompletion() string {
	var b strings.Builder
	b.WriteString(`#compdef es-tool

_es_tool_completion() {
    local -a commands options
    commands=(
`)
	for _, command := range completionCommands {
		fmt.Fprintf(&b, "        %s\n", shellSingleQuote(command.name+":"+command.description))
	}
	b.WriteString(`        '-h:show help'
        '--help:show help'
        '-v:show version'
        '--version:show version'
    )

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        return
    fi

    if [[ "$words[2]" == "completion" && CURRENT -eq 3 ]]; then
        if [[ "$PREFIX" == -* ]]; then
            options=('-h:show help' '--help:show help')
            _describe 'option' options
        else
            _values 'shell' bash zsh fish
        fi
        return
    fi

    options=()
    case "$words[2]" in
`)
	for _, command := range completionCommands {
		options := completionOptionDescriptions(command)
		if len(options) == 0 {
			continue
		}
		fmt.Fprintf(&b, "        %s)\n            options=(\n", command.name)
		for _, option := range options {
			fmt.Fprintf(&b, "                %s\n", shellSingleQuote(option))
		}
		b.WriteString("            )\n            ;;\n")
	}
	b.WriteString(`    esac

    case "$words[CURRENT-1]" in
        --body|--doc)
            _files
            return
            ;;
    esac

    if [[ "$PREFIX" == -* ]]; then
        _describe 'option' options
    fi
}

compdef _es_tool_completion es-tool
`)
	return b.String()
}

func fishCompletion() string {
	var b strings.Builder
	b.WriteString(`# fish completion for es-tool
function __fish_es_tool_needs_command
    set -l tokens (commandline -opc)
    test (count $tokens) -eq 1
end

function __fish_es_tool_using_command
    set -l tokens (commandline -opc)
    test (count $tokens) -ge 2; and test "$tokens[2]" = "$argv[1]"
end

complete -c es-tool -f -n '__fish_es_tool_needs_command' -s h -l help -d 'show help'
complete -c es-tool -f -n '__fish_es_tool_needs_command' -s v -l version -d 'show version'
`)
	for _, command := range completionCommands {
		fmt.Fprintf(&b, "complete -c es-tool -f -n '__fish_es_tool_needs_command' -a %s -d %s\n",
			shellSingleQuote(command.name), shellSingleQuote(command.description))
	}
	b.WriteByte('\n')
	for _, command := range completionCommands {
		condition := shellSingleQuote("__fish_es_tool_using_command " + command.name)
		if command.flagHelp {
			fmt.Fprintf(&b, "complete -c es-tool -f -n %s -s h -l help -d 'show help'\n", condition)
		}
		for _, option := range command.options {
			fileFlag := " -f"
			valueFlag := ""
			switch {
			case option.fileValue:
				valueFlag = " -r"
				fileFlag = ""
			case option.takesValue:
				valueFlag = " -x"
			}
			fmt.Fprintf(&b, "complete -c es-tool%s -n %s -l %s%s -d %s\n",
				fileFlag, condition, option.name, valueFlag, shellSingleQuote(option.description))
		}
	}
	b.WriteString("\ncomplete -c es-tool -f -n '__fish_es_tool_using_command completion' -a 'bash zsh fish'\n")
	return b.String()
}

func completionOptionNames(command completionCommand) []string {
	options := make([]string, 0, len(command.options)+2)
	if command.flagHelp {
		options = append(options, "-h", "--help")
	}
	for _, option := range command.options {
		options = append(options, "--"+option.name)
	}
	return options
}

func completionOptionDescriptions(command completionCommand) []string {
	options := make([]string, 0, len(command.options)+2)
	if command.flagHelp {
		options = append(options, "-h:show help", "--help:show help")
	}
	for _, option := range command.options {
		options = append(options, "--"+option.name+":"+option.description)
	}
	return options
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
