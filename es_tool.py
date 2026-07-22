#!/usr/bin/env python3
"""es_tool — a small, zero-dependency CLI/REPL to look at and edit
state in Elasticsearch.

It speaks the ES REST API directly using ``urllib`` so it works without
the ``elasticsearch`` Python client. Authentication is optional and can
be provided via an API key, basic auth, or none (default for the local
``elastic-package`` stack).

Configuration (env vars, all optional):
    ES_URL         Base URL of Elasticsearch (default: http://localhost:9202)
    ES_API_KEY     Encoded API key (sent as ``Authorization: ApiKey ...``)
    ES_USER        Basic-auth user (used with ES_PASSWORD)
    ES_PASSWORD    Basic-auth password
    ES_VERIFY_TLS  "0"/"false" to disable TLS verification (default: on)

Quick start:
    # one-shot CLI
    python3 es_tool.py ping
    python3 es_tool.py indices
    python3 es_tool.py search sentinel-fix-runs --size 5
    python3 es_tool.py get sentinel-fix-runs <doc_id>
    python3 es_tool.py edit sentinel-fix-runs <doc_id>
    python3 es_tool.py update sentinel-fix-runs <doc_id> --set status=cancelled
    python3 es_tool.py delete sentinel-fix-runs <doc_id>
    python3 es_tool.py count sentinel-fix-runs --q 'status:running'

    # interactive REPL
    python3 es_tool.py repl

    # full-screen interactive TUI (select / view / edit / delete)
    python3 es_tool.py tui
    python3 es_tool.py tui --index sentinel-fix-runs
"""
from __future__ import annotations

import argparse
import base64
import json
import os
import shlex
import ssl
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any


DEFAULT_ES_URL = "http://localhost:9202"


# ---------------------------------------------------------------------------
# HTTP client
# ---------------------------------------------------------------------------


@dataclass
class EsClient:
    base_url: str
    api_key: str | None = None
    user: str | None = None
    password: str | None = None
    verify_tls: bool = True

    @classmethod
    def from_env(cls) -> "EsClient":
        verify_env = os.environ.get("ES_VERIFY_TLS", "1").lower()
        return cls(
            base_url=os.environ.get("ES_URL", DEFAULT_ES_URL).rstrip("/"),
            api_key=os.environ.get("ES_API_KEY") or None,
            user=os.environ.get("ES_USER") or None,
            password=os.environ.get("ES_PASSWORD") or None,
            verify_tls=verify_env not in {"0", "false", "no"},
        )

    def _headers(self) -> dict[str, str]:
        headers = {"Content-Type": "application/json",
                   "Accept": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"ApiKey {self.api_key}"
        elif self.user is not None:
            token = base64.b64encode(
                f"{self.user}:{self.password or ''}".encode()
            ).decode()
            headers["Authorization"] = f"Basic {token}"
        return headers

    def request(
        self,
        method: str,
        path: str,
        body: Any = None,
        params: dict[str, Any] | None = None,
    ) -> tuple[int, Any]:
        """Perform a request. Returns (status_code, parsed_json_or_text)."""
        url = f"{self.base_url}/{path.lstrip('/')}"
        if params:
            # ES uses query strings for things like ?pretty, ?refresh, ?q=...
            qs = urllib.parse.urlencode(
                {k: v for k, v in params.items() if v is not None}, doseq=True
            )
            url = f"{url}?{qs}" if qs else url

        data: bytes | None = None
        if body is not None:
            data = body.encode() if isinstance(body, str) else json.dumps(body).encode()

        req = urllib.request.Request(url=url, data=data, method=method.upper())
        for k, v in self._headers().items():
            req.add_header(k, v)

        ctx: ssl.SSLContext | None = None
        if url.startswith("https://") and not self.verify_tls:
            ctx = ssl.create_default_context()
            ctx.check_hostname = False
            ctx.verify_mode = ssl.CERT_NONE

        try:
            with urllib.request.urlopen(req, context=ctx, timeout=30) as resp:
                raw = resp.read().decode("utf-8", errors="replace")
                status = resp.status
        except urllib.error.HTTPError as e:
            raw = e.read().decode("utf-8", errors="replace")
            status = e.code
        except urllib.error.URLError as e:
            raise SystemExit(f"connection error: {e.reason} ({url})")

        if not raw:
            return status, None
        try:
            return status, json.loads(raw)
        except json.JSONDecodeError:
            return status, raw


# ---------------------------------------------------------------------------
# Pretty-printing helpers
# ---------------------------------------------------------------------------


def dump(data: Any) -> str:
    if isinstance(data, str):
        return data
    return json.dumps(data, indent=2, sort_keys=True, ensure_ascii=False)


def print_json(data: Any) -> None:
    print(dump(data))


def err(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)


def ok_or_die(status: int, body: Any) -> Any:
    if 200 <= status < 300:
        return body
    err(f"HTTP {status}")
    print_json(body)
    sys.exit(1)


# ---------------------------------------------------------------------------
# High-level operations
# ---------------------------------------------------------------------------


def cmd_ping(client: EsClient, _args: argparse.Namespace) -> None:
    status, body = client.request("GET", "/")
    ok_or_die(status, body)
    print_json(body)


def cmd_indices(client: EsClient, args: argparse.Namespace) -> None:
    params = {"format": "json", "v": "true"}
    path = f"/_cat/indices/{args.pattern}" if args.pattern else "/_cat/indices"
    status, body = client.request("GET", path, params=params)
    ok_or_die(status, body)
    if not isinstance(body, list):
        print_json(body)
        return
    # Print a compact table: index, docs.count, store.size, status, health.
    rows = sorted(body, key=lambda r: r.get("index", ""))
    cols = ("health", "status", "index", "docs.count", "store.size")
    widths = {c: max(len(c), *(len(str(r.get(c, "")))
                     for r in rows)) for c in cols}
    header = "  ".join(c.ljust(widths[c]) for c in cols)
    print(header)
    print("  ".join("-" * widths[c] for c in cols))
    for r in rows:
        print("  ".join(str(r.get(c, "")).ljust(widths[c]) for c in cols))


def cmd_mapping(client: EsClient, args: argparse.Namespace) -> None:
    status, body = client.request("GET", f"/{args.index}/_mapping")
    ok_or_die(status, body)
    print_json(body)


def cmd_count(client: EsClient, args: argparse.Namespace) -> None:
    params = {"q": args.q} if args.q else None
    status, body = client.request(
        "GET", f"/{args.index}/_count", params=params)
    ok_or_die(status, body)
    print_json(body)


def cmd_search(client: EsClient, args: argparse.Namespace) -> None:
    params: dict[str, Any] = {"size": args.size}
    if args.q:
        params["q"] = args.q
    if args.sort:
        params["sort"] = args.sort
    if args.source:
        params["_source"] = args.source

    body: Any = None
    if args.body:
        body = _load_json_arg(args.body)

    status, resp = client.request(
        "GET" if body is None else "POST",
        f"/{args.index}/_search",
        body=body,
        params=params,
    )
    ok_or_die(status, resp)

    if args.ids_only and isinstance(resp, dict):
        for h in resp.get("hits", {}).get("hits", []):
            print(h.get("_id"))
        return
    print_json(resp)


def cmd_get(client: EsClient, args: argparse.Namespace) -> None:
    status, body = client.request("GET", f"/{args.index}/_doc/{args.doc_id}")
    ok_or_die(status, body)
    print_json(body)


def cmd_index(client: EsClient, args: argparse.Namespace) -> None:
    """Create/replace a document with a known id, or POST a new one when id is omitted."""
    body = _load_json_arg(args.body)
    refresh = "true" if args.refresh else None
    if args.doc_id:
        path = f"/{args.index}/_doc/{args.doc_id}"
        method = "PUT"
    else:
        path = f"/{args.index}/_doc"
        method = "POST"
    status, resp = client.request(
        method, path, body=body, params={"refresh": refresh})
    ok_or_die(status, resp)
    print_json(resp)


def cmd_update(client: EsClient, args: argparse.Namespace) -> None:
    """Partial update via ``_update`` using ``--set k=v`` pairs or ``--doc`` JSON."""
    if not args.set and not args.doc:
        raise SystemExit("update: provide --set k=v [...] or --doc <json>")
    doc: dict[str, Any] = {}
    if args.doc:
        loaded = _load_json_arg(args.doc)
        if not isinstance(loaded, dict):
            raise SystemExit("--doc must be a JSON object")
        doc.update(loaded)
    for kv in args.set or []:
        if "=" not in kv:
            raise SystemExit(f"--set expects key=value, got {kv!r}")
        k, v = kv.split("=", 1)
        doc[k] = _coerce_scalar(v)

    body = {"doc": doc, "doc_as_upsert": bool(args.upsert)}
    refresh = "true" if args.refresh else None
    status, resp = client.request(
        "POST",
        f"/{args.index}/_update/{args.doc_id}",
        body=body,
        params={"refresh": refresh},
    )
    ok_or_die(status, resp)
    print_json(resp)


def cmd_delete(client: EsClient, args: argparse.Namespace) -> None:
    if not args.yes:
        confirm = input(
            f"delete {args.index}/{args.doc_id}? [y/N] ").strip().lower()
        if confirm not in {"y", "yes"}:
            print("aborted")
            return
    refresh = "true" if args.refresh else None
    status, body = client.request(
        "DELETE",
        f"/{args.index}/_doc/{args.doc_id}",
        params={"refresh": refresh},
    )
    ok_or_die(status, body)
    print_json(body)


def cmd_delete_by_query(client: EsClient, args: argparse.Namespace) -> None:
    if not args.q and not args.body:
        raise SystemExit("delete-by-query: pass --q or --body")
    if not args.yes:
        confirm = input(
            f"delete-by-query on {args.index} (q={args.q!r})? [y/N] "
        ).strip().lower()
        if confirm not in {"y", "yes"}:
            print("aborted")
            return
    params = {"q": args.q, "refresh": "true" if args.refresh else None}
    body = _load_json_arg(args.body) if args.body else None
    status, resp = client.request(
        "POST", f"/{args.index}/_delete_by_query", body=body, params=params
    )
    ok_or_die(status, resp)
    print_json(resp)


def cmd_tui(client: EsClient, args: argparse.Namespace) -> None:
    """Launch the curses-based interactive browser."""
    try:
        from es_tui import run_tui
    except ImportError as e:
        raise SystemExit(f"tui module unavailable: {e}") from e
    run_tui(client, start_index=getattr(args, "index", None))


def cmd_edit(client: EsClient, args: argparse.Namespace) -> None:
    """Fetch a document, open the source in $EDITOR, and PUT it back."""
    status, body = client.request("GET", f"/{args.index}/_doc/{args.doc_id}")
    ok_or_die(status, body)
    if not isinstance(body, dict) or "_source" not in body:
        raise SystemExit(f"unexpected response: {body!r}")

    original = body["_source"]
    edited = _edit_in_editor(original)
    if edited == original:
        print("no changes — nothing to do")
        return

    refresh = "true" if args.refresh else None
    params = {"refresh": refresh}
    # If_seq_no/_primary_term are present, use optimistic concurrency control.
    if "_seq_no" in body and "_primary_term" in body:
        params["if_seq_no"] = body["_seq_no"]
        params["if_primary_term"] = body["_primary_term"]

    status, resp = client.request(
        "PUT", f"/{args.index}/_doc/{args.doc_id}", body=edited, params=params
    )
    ok_or_die(status, resp)
    print_json(resp)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _coerce_scalar(value: str) -> Any:
    """Coerce CLI --set values: JSON literals first, otherwise raw string."""
    try:
        return json.loads(value)
    except json.JSONDecodeError:
        return value


def _load_json_arg(value: str) -> Any:
    """Accept either an inline JSON string or @path/to/file.json."""
    if value.startswith("@"):
        path = value[1:]
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    try:
        return json.loads(value)
    except json.JSONDecodeError as e:
        raise SystemExit(f"invalid JSON: {e}") from e


def _edit_in_editor(initial: Any) -> Any:
    editor = os.environ.get("EDITOR") or os.environ.get("VISUAL") or "vi"
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".json", delete=False, encoding="utf-8"
    ) as tmp:
        json.dump(initial, tmp, indent=2, sort_keys=True, ensure_ascii=False)
        tmp.write("\n")
        tmp_path = tmp.name
    try:
        # shlex.split lets users export EDITOR="code --wait"
        proc = subprocess.run(shlex.split(editor) + [tmp_path], check=False)
        if proc.returncode != 0:
            raise SystemExit(f"editor exited with {proc.returncode}")
        with open(tmp_path, encoding="utf-8") as f:
            text = f.read()
        try:
            return json.loads(text)
        except json.JSONDecodeError as e:
            raise SystemExit(f"edited file is not valid JSON: {e}") from e
    finally:
        try:
            os.unlink(tmp_path)
        except OSError:
            pass


# ---------------------------------------------------------------------------
# REPL
# ---------------------------------------------------------------------------


REPL_HELP = """\
Commands (run `<command> --help` for details):
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
  quit / exit        # leave the REPL
"""


def run_repl(client: EsClient) -> None:
    parser = build_parser()  # reuse the same arg parser for parsing REPL lines
    print(f"es_tool REPL — connected to {client.base_url}")
    print("type 'help' for commands, 'quit' to exit\n")
    while True:
        try:
            line = input("es> ").strip()
        except (EOFError, KeyboardInterrupt):
            print()
            return
        if not line:
            continue
        if line in {"quit", "exit"}:
            return
        if line == "help":
            print(REPL_HELP)
            continue
        if line == "whoami":
            print(f"url   : {client.base_url}")
            print(
                f"auth  : {'apikey' if client.api_key else 'basic' if client.user else 'none'}")
            continue
        if line.startswith("use "):
            client.base_url = line.split(None, 1)[1].rstrip("/")
            print(f"now using {client.base_url}")
            continue

        try:
            tokens = shlex.split(line)
            args = parser.parse_args(tokens)
        except SystemExit:
            # argparse calls sys.exit on bad input; swallow it so the REPL stays alive
            continue
        except ValueError as e:
            err(str(e))
            continue

        try:
            args.func(client, args)
        except SystemExit as e:
            if e.code not in (None, 0):
                err(str(e))
        except Exception as e:  # pragma: no cover - REPL guard
            err(f"{type(e).__name__}: {e}")


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="es_tool",
        description="Tiny CLI/REPL to view and edit Elasticsearch state.",
    )
    sub = p.add_subparsers(dest="cmd", required=True)

    sp = sub.add_parser("ping", help="GET / (cluster info)")
    sp.set_defaults(func=cmd_ping)

    sp = sub.add_parser(
        "indices", help="list indices (optionally filtered by pattern)")
    sp.add_argument("pattern", nargs="?")
    sp.set_defaults(func=cmd_indices)

    sp = sub.add_parser("mapping", help="get an index's mapping")
    sp.add_argument("index")
    sp.set_defaults(func=cmd_mapping)

    sp = sub.add_parser(
        "count", help="count docs (optionally Lucene-filtered)")
    sp.add_argument("index")
    sp.add_argument("--q", help="Lucene query string")
    sp.set_defaults(func=cmd_count)

    sp = sub.add_parser(
        "search", help="run _search (Lucene query or JSON body)")
    sp.add_argument("index")
    sp.add_argument("--q", help="Lucene query string")
    sp.add_argument("--size", type=int, default=10)
    sp.add_argument("--sort", help="sort, e.g. @timestamp:desc")
    sp.add_argument("--source", help="comma-separated _source includes")
    sp.add_argument("--body", help="JSON body (or @file)")
    sp.add_argument("--ids-only", action="store_true",
                    help="print only hit _ids")
    sp.set_defaults(func=cmd_search)

    sp = sub.add_parser("get", help="fetch a document by id")
    sp.add_argument("index")
    sp.add_argument("doc_id")
    sp.set_defaults(func=cmd_get)

    sp = sub.add_parser("index", help="index (create/replace) a document")
    sp.add_argument("index")
    sp.add_argument("doc_id", nargs="?")
    sp.add_argument("--body", required=True, help="JSON body (or @file)")
    sp.add_argument("--refresh", action="store_true")
    sp.set_defaults(func=cmd_index)

    sp = sub.add_parser("update", help="partial update (POST _update)")
    sp.add_argument("index")
    sp.add_argument("doc_id")
    sp.add_argument("--set", action="append", metavar="K=V",
                    help="field assignment (repeatable; JSON literals accepted)")
    sp.add_argument("--doc", help="JSON object (or @file) to merge")
    sp.add_argument("--upsert", action="store_true",
                    help="create when missing (doc_as_upsert)")
    sp.add_argument("--refresh", action="store_true")
    sp.set_defaults(func=cmd_update)

    sp = sub.add_parser(
        "edit", help="fetch a doc, open in $EDITOR, write back with OCC")
    sp.add_argument("index")
    sp.add_argument("doc_id")
    sp.add_argument("--refresh", action="store_true")
    sp.set_defaults(func=cmd_edit)

    sp = sub.add_parser("delete", help="delete a document by id")
    sp.add_argument("index")
    sp.add_argument("doc_id")
    sp.add_argument("--yes", action="store_true",
                    help="skip confirmation prompt")
    sp.add_argument("--refresh", action="store_true")
    sp.set_defaults(func=cmd_delete)

    sp = sub.add_parser("delete-by-query", help="delete docs matching a query")
    sp.add_argument("index")
    sp.add_argument("--q", help="Lucene query string")
    sp.add_argument("--body", help="JSON body (or @file)")
    sp.add_argument("--yes", action="store_true",
                    help="skip confirmation prompt")
    sp.add_argument("--refresh", action="store_true")
    sp.set_defaults(func=cmd_delete_by_query)

    sp = sub.add_parser("repl", help="start interactive REPL")
    sp.set_defaults(func=lambda client, args: run_repl(client))

    sp = sub.add_parser(
        "tui",
        help="full-screen interactive browser (select/view/edit/delete docs)",
    )
    sp.add_argument(
        "--index",
        help="skip the indices screen and jump straight into this index",
    )
    sp.set_defaults(func=cmd_tui)

    return p


def main(argv: list[str] | None = None) -> None:
    parser = build_parser()
    args = parser.parse_args(argv)
    client = EsClient.from_env()
    args.func(client, args)


if __name__ == "__main__":
    main()
