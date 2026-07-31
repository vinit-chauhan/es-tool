# es-tool

A full-screen terminal UI to **browse, edit, and query Elasticsearch**. Talks
to the ES REST API directly via `net/http`; the interface is built on
[Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles), and
[Lipgloss](https://github.com/charmbracelet/lipgloss).

es-tool has a single interface — there is no separate CLI or REPL. Every
capability (search, count, create, partial update, delete-by-query, cluster
info) lives in the TUI.

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
│   ├── tui/              # Bubble Tea application (the only interface)
│   └── util/             # shared JSON / shell / editor helpers
├── go.mod
└── README.md
```

## Usage

```bash
# Optional: configure connection (defaults shown)
export ES_URL=http://localhost:9202     # your ES endpoint
# export ES_API_KEY=<base64-encoded api key>
# export ES_USER=elastic
# export ES_PASSWORD=changeme
# export ES_VERIFY_TLS=0                # only if using self-signed certs

./es-tool                              # start at the indices list
./es-tool --index sentinel-fix-runs    # jump straight into one index
./es-tool --version                    # print version and exit
```

If no `ES_*` env vars are set, es-tool falls back to the active saved cluster
profile (see Settings below).

## Keys

| Screen          | Keys                                                                                                                                                                               |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Indices          | `↑/↓` move · `Enter` open · `/` filter · `h` toggle hidden · `S` index details · `i` cluster info · `r` refresh · `.` settings · `q` quit                                        |
| Index details    | `Tab`/`←/→` settings ↔ mappings · `r` refresh · `b`/`Esc` back                                                                                                                    |
| Documents        | `Enter` view · `c` create · `e` replace · `u`/`U` partial update / upsert · `d`/`D` delete doc / delete-by-query · `/` client filter · `f` server query · `F` advanced search · `n/p` page · `s` page size · `r` refresh |
| Document viewer  | `e` replace · `u`/`U` partial update / upsert · `d` delete · `w` toggle wrap · `↑/↓` `PgUp/PgDn` scroll · `b`/`Esc` back                                                          |
| Advanced search  | `f` Lucene query · `s` sort · `o` `_source` filter · `j` raw JSON body · `i` IDs only · `c` exact count · `x` reset · `Enter` run · `b` back                                     |
| Cluster info     | `r` refresh · `↑/↓` `PgUp/PgDn` scroll · `b`/`Esc` back                                                                                                                            |
| Settings         | `Enter` activate profile · `a` add · `e` edit · `d` delete · `c` quick connect (session-only) · `r` check health · `b`/`Esc` back                                                |

Global: `?` toggles a full hotkey reference for the current session; `.`
jumps to settings from anywhere; `Ctrl+C`/`q` quits.

### Notes

- **Document editing** (`e`/`u`/`U`) opens `$EDITOR` on the document JSON and
  suspends the TUI cleanly; writes use optimistic concurrency control
  (`if_seq_no`/`if_primary_term`) so concurrent writers won't be silently
  overwritten. `u` merges the edited object as a partial `_update`; `U` does
  the same with `doc_as_upsert`.
- **Create** (`c`) prompts for an optional document id, opens `$EDITOR` with an
  empty JSON template, and indexes on save.
- **Delete-by-query** (`D`) runs an exact `_count` for the active query first
  and requires typing `delete <n>` to confirm before calling
  `_delete_by_query`.
- **Advanced search** (`F`) builds a Lucene query, sort, `_source` filter, an
  ids-only toggle, or a hand-edited JSON request body; `c` runs an exact
  `_count` for the same query instead of fetching hits.
- **Cluster info** (`i` from the indices screen) shows `GET /`, cluster
  health, connection details, and (when the security feature is enabled) the
  authenticated user.
- `/` on indices or documents does a fast client-side substring filter on the
  visible rows; `f` on documents sets a real Lucene `q=` filter on the server.
  Matched substrings are underlined in the document list.
- Hidden and dot-prefixed indices are omitted by default; press `h` to
  toggle them.
- Pagination uses `from`/`size`; the header shows the total so you can tell
  when you've reached the end.
- JSON in the document viewer and index details panes is syntax-highlighted.

## Configuration

| Env var         | Default                 | Meaning                                       |
| --------------- | ------------------------ | ---------------------------------------------|
| `ES_URL`        | `http://localhost:9202`  | Base URL of Elasticsearch                     |
| `ES_API_KEY`    | —                        | Encoded API key (`Authorization: ApiKey ...`) |
| `ES_USER`       | —                        | Basic-auth user (used with `ES_PASSWORD`)     |
| `ES_PASSWORD`   | —                        | Basic-auth password                           |
| `ES_VERIFY_TLS` | `1`                      | `0`/`false`/`no` disables TLS verification    |

Settings (`.`) stores named cluster profiles in `es-tool/config.json` under
the platform's user config directory. The file can contain API keys or
passwords and is written with owner-only (`0600`) permissions; secret fields
are masked in the TUI. `c` on the settings screen opens a quick-connect prompt
that configures the client for the current session only, without saving a
profile.

The active saved profile is used on the next launch. Explicit `ES_*`
environment variables take precedence at startup; activating a profile from
settings switches the current session to it.
