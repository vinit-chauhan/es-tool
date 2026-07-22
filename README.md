# es-data-viewer

Tiny zero-dependency Python tool to **view and edit state in Elasticsearch**.
Talks to the ES REST API via stdlib `urllib` — no `pip install` required.

## Setup

```bash
# Optional: configure connection (defaults shown)
export ES_URL=http://localhost:9202     # your ES endpoint
# export ES_API_KEY=<base64-encoded api key>
# export ES_USER=elastic
# export ES_PASSWORD=changeme
# export ES_VERIFY_TLS=0                # only if using self-signed certs

cd /tmp/es-tool
python3 es_tool.py --help
```

## CLI examples

```bash
# Cluster info
python3 es_tool.py ping

# List indices (optional pattern)
python3 es_tool.py indices 'sentinel-*'

# Inspect a mapping
python3 es_tool.py mapping sentinel-fix-runs

# Count and search
python3 es_tool.py count  sentinel-fix-runs --q 'status:running'
python3 es_tool.py search sentinel-fix-runs --q 'status:running' --size 5 --sort '@timestamp:desc'
python3 es_tool.py search sentinel-fix-runs --body '{"query":{"match_all":{}}}' --ids-only

# Look at one document
python3 es_tool.py get sentinel-fix-runs <doc_id>

# Edit a document interactively in $EDITOR (uses _seq_no/_primary_term for OCC)
EDITOR=vim python3 es_tool.py edit sentinel-fix-runs <doc_id> --refresh

# Targeted partial updates without an editor
python3 es_tool.py update sentinel-fix-runs <doc_id> \
    --set status=cancelled --set 'iterations=3' --refresh

# Or merge a JSON object
python3 es_tool.py update sentinel-fix-runs <doc_id> \
    --doc '{"status":"failed","error":"manual override"}' --refresh

# Create/replace a whole document
python3 es_tool.py index sentinel-fix-runs my-id --body @doc.json --refresh

# Delete (with confirmation)
python3 es_tool.py delete sentinel-fix-runs <doc_id>
python3 es_tool.py delete-by-query sentinel-fix-runs --q 'status:cancelled' --yes
```

## Interactive TUI (recommended)

Full-screen browser to **select / view / edit / delete** documents with the
keyboard. Uses Python's stdlib `curses` (no extra installs on macOS/Linux).

```bash
python3 es_tool.py tui                       # start at the indices list
python3 es_tool.py tui --index sentinel-fix-runs   # jump straight into one
```

Keys:

| Screen  | Keys                                                                                                                                                        |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Indices | `↑/↓` `j/k` move · `Enter` open · `/` filter · `r` refresh · `q` quit                                                                                       |
| Docs    | `↑/↓` page nav · `Enter`/`v` view · `e` edit · `d` delete · `/` filter · `f` Lucene query · `n/p` page · `s` size · `r` refresh · `b`/`Esc` back · `q` quit |
| Viewer  | `↑/↓` `PgUp/PgDn` `g/G` scroll · `e` edit · `d` delete · `b`/`Esc` back · `q` quit                                                                          |

- `e` opens the document JSON in `$EDITOR` (curses suspends cleanly), and writes
  it back using optimistic concurrency control (`if_seq_no`/`if_primary_term`).
- `d` prompts before deleting and refreshes with `?refresh=true`.
- `/` does a fast client-side substring filter on the visible rows; `f` sets a
  real Lucene `q=` filter on the server.
- Pagination uses `from`/`size`; the header shows `total` so you can tell when
  you've reached the end.

## Interactive REPL (line-oriented)

```bash
python3 es_tool.py repl
```

Inside the REPL:

```
es> indices sentinel-*
es> get sentinel-fix-runs abc123
es> edit sentinel-fix-runs abc123 --refresh
es> update sentinel-fix-runs abc123 --set status=cancelled --refresh
es> use http://localhost:9201          # switch cluster on the fly
es> help
es> quit
```

## Notes

- `--body` arguments accept inline JSON **or** `@path/to/file.json`.
- `--set k=v` parses values as JSON when possible (`true`, `42`, `"text"`),
  falling back to a raw string. Use `--doc '{...}'` for nested objects.
- `edit` uses optimistic concurrency control (`if_seq_no`/`if_primary_term`)
  so concurrent writers won't be silently overwritten.
- Mutating commands (`delete`, `delete-by-query`) prompt for confirmation;
  pass `--yes` to skip in scripts.
