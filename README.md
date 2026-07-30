# es-tool

Tiny Go tool to **view and edit state in Elasticsearch**. Talks to the ES REST
API directly via `net/http` — the only dependency is
[`tcell`](https://github.com/gdamore/tcell) for the full-screen TUI.

## Build

```bash
make build        # builds ./bin/es-tool (with version stamped from git)
make install      # go install onto your GOPATH/bin
make test         # run tests
make check        # fmt-check + vet + test (CI gate)
make help         # list all targets

# ...or without make:
go build -o es-tool ./cmd/es-tool
go install github.com/vinit-chauhan/es-tool/cmd/es-tool@latest
```

## Project layout

Standard Go layout — the binary lives under `cmd/`, all logic under `internal/`:

```
es-tool/
├── cmd/es-tool/          # main package (entrypoint)
│   └── main.go
├── internal/
│   ├── esclient/         # Elasticsearch REST client (net/http)
│   ├── cli/              # subcommand dispatch, commands, REPL
│   ├── tui/              # full-screen tcell browser
│   └── util/             # shared JSON / shell / editor helpers
├── go.mod
└── README.md
```

## Setup

```bash
# Optional: configure connection (defaults shown)
export ES_URL=http://localhost:9202     # your ES endpoint
# export ES_API_KEY=<base64-encoded api key>
# export ES_USER=elastic
# export ES_PASSWORD=changeme
# export ES_VERIFY_TLS=0                # only if using self-signed certs

./es-tool          # prints usage
```

## Shell completion

Generate command and flag completions for Bash, Zsh, or Fish:

```bash
# Bash
source <(es-tool completion bash)

# Zsh (after compinit)
autoload -Uz compinit && compinit
source <(es-tool completion zsh)

# Fish
es-tool completion fish | source
```

Add the command for your shell to its startup file to enable completion in
future sessions.

## CLI examples

```bash
# Cluster info
./es-tool ping

# List indices (optional pattern)
./es-tool indices 'sentinel-*'

# Inspect a mapping
./es-tool mapping sentinel-fix-runs

# Count and search
./es-tool count  sentinel-fix-runs --q 'status:running'
./es-tool search sentinel-fix-runs --q 'status:running' --size 5 --sort '@timestamp:desc'
./es-tool search sentinel-fix-runs --body '{"query":{"match_all":{}}}' --ids-only

# Look at one document
./es-tool get sentinel-fix-runs <doc_id>

# Edit a document interactively in $EDITOR (uses _seq_no/_primary_term for OCC)
EDITOR=vim ./es-tool edit sentinel-fix-runs <doc_id> --refresh

# Targeted partial updates without an editor
./es-tool update sentinel-fix-runs <doc_id> \
    --set status=cancelled --set 'iterations=3' --refresh

# Or merge a JSON object
./es-tool update sentinel-fix-runs <doc_id> \
    --doc '{"status":"failed","error":"manual override"}' --refresh

# Create/replace a whole document
./es-tool index sentinel-fix-runs my-id --body @doc.json --refresh

# Delete (with confirmation)
./es-tool delete sentinel-fix-runs <doc_id>
./es-tool delete-by-query sentinel-fix-runs --q 'status:cancelled' --yes
```

## Interactive TUI (recommended)

Full-screen browser to **select / view / edit / delete** documents with the
keyboard. Built on `tcell` (works on Linux/macOS terminals).

```bash
./es-tool tui                              # start at the indices list
./es-tool tui --index sentinel-fix-runs    # jump straight into one
```

Keys:

| Screen   | Keys                                                                                                                                                                                           |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Indices  | `↑/↓` `j/k` move · `Enter` open · `/` filter · `h` hidden indices · `r` refresh · `S` index details · `.` settings · `q` quit                                                                  |
| Docs     | `↑/↓` page nav · `Enter`/`v` view · `e` edit · `d` delete · `/` filter · `f` Lucene query · `n/p` page · `s` size · `r` refresh · `S` index details · `.` settings · `b`/`Esc` back · `q` quit |
| Viewer   | `↑/↓` `PgUp/PgDn` `g/G` scroll · `e` edit · `d` delete · `S` index details · `.` settings · `b`/`Esc` back · `q` quit                                                                          |
| Settings | `↑/↓` `j/k` move · `Enter` use cluster · `a` add · `e` edit · `r` refresh health · `b`/`Esc` back · `q` quit                                                                                   |

- Press `?` from any TUI screen to open the scrollable hotkey reference.
- Press `S` to inspect the selected/current index settings and mappings; press
  `.` to open cluster settings.
- The top-right header indicator shows cluster health (`green`, `yellow`, or
  `red`) and reports authentication or connection failures.
- Inline setting editors support `←/→`, `Home/End`, `Backspace/Delete`, and
  `Ctrl+U` to clear the value.
- Hidden Elasticsearch indices and dot-prefixed indices are omitted by default;
  press `h` on the indices screen to toggle them.
- `e` opens the document JSON in `$EDITOR` (the TUI suspends cleanly), and writes
  it back using optimistic concurrency control (`if_seq_no`/`if_primary_term`).
- `d` prompts before deleting and refreshes with `?refresh=true`.
- `/` does a fast client-side substring filter on the visible rows; `f` sets a
  real Lucene `q=` filter on the server.
- Pagination uses `from`/`size`; the header shows `total` so you can tell when
  you've reached the end.

## Interactive REPL (line-oriented)

```bash
./es-tool repl
```

Inside the REPL:

```
es> indices sentinel-*
es> get sentinel-fix-runs abc123
es> edit sentinel-fix-runs abc123 --refresh
es> update sentinel-fix-runs abc123 --set status=cancelled --refresh
es> use http://localhost:9201          # switch cluster on the fly
es> whoami
es> help
es> quit
```

## Notes

- `--body` arguments accept inline JSON **or** `@path/to/file.json`.
- `--set k=v` parses values as JSON when possible (`true`, `42`, `"text"`,
  `[...]`, `{...}`), falling back to a raw string. Use `--doc '{...}'` for
  nested objects.
- Flags may appear before or after positional arguments
  (e.g. `count sentinel-fix-runs --q '*'`).
- `edit` uses optimistic concurrency control (`if_seq_no`/`if_primary_term`)
  so concurrent writers won't be silently overwritten.
- Mutating commands (`delete`, `delete-by-query`) prompt for confirmation;
  pass `--yes` to skip in scripts.

## Configuration

| Env var         | Default                 | Meaning                                       |
| --------------- | ----------------------- | --------------------------------------------- |
| `ES_URL`        | `http://localhost:9202` | Base URL of Elasticsearch                     |
| `ES_API_KEY`    | —                       | Encoded API key (`Authorization: ApiKey ...`) |
| `ES_USER`       | —                       | Basic-auth user (used with `ES_PASSWORD`)     |
| `ES_PASSWORD`   | —                       | Basic-auth password                           |
| `ES_VERIFY_TLS` | `1`                     | `0`/`false`/`no` disables TLS verification    |

The TUI settings page (`S`) stores named cluster profiles in
`es-tool/config.json` under the platform's user config directory. The file can
contain API keys or passwords and is written with owner-only (`0600`)
permissions. Secret fields are masked in the TUI.

The active saved profile is used on the next TUI launch. Explicit `ES_*`
environment variables take precedence at startup; choosing a profile from the
settings page switches the current session to it.
