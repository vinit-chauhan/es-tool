"""Interactive curses-based TUI for es_tool.

Layout / navigation:

    [Indices screen]
        ↑/↓ or j/k   move
        Enter        open index → docs screen
        /            filter indices by substring
        r            refresh list
        q            quit

    [Docs screen]
        ↑/↓ j/k      move
        PgUp/PgDn    page
        g / G        first / last
        Enter or v   view document JSON
        e            edit document in $EDITOR (writes back with OCC)
        d            delete (with confirmation)
        /            filter hits by id/source substring
        f            set Lucene query (q=)
        n            next page (size+from pagination)
        p            previous page
        s            change page size
        r            refresh
        b / Esc      back to indices
        q            quit

    [Viewer screen]
        ↑/↓ j/k      scroll
        PgUp/PgDn    page
        g / G        top / bottom
        e            open in $EDITOR
        d            delete
        b / Esc / q  back

Notes
-----
This module uses only the standard library (``curses``). It works on Linux/
macOS terminals. Windows users need ``windows-curses``.
"""
from __future__ import annotations

import curses
import json
import os
import shlex
import subprocess
import tempfile
from dataclasses import dataclass, field
from typing import Any, Callable

from es_tool import EsClient


# ---------------------------------------------------------------------------
# Small UI primitives
# ---------------------------------------------------------------------------


@dataclass
class Status:
    """Transient status message shown at the bottom of the screen."""

    text: str = ""
    is_error: bool = False

    def set(self, text: str, *, error: bool = False) -> None:
        self.text = text
        self.is_error = error

    def clear(self) -> None:
        self.text = ""
        self.is_error = False


def _safe_addstr(win: "curses._CursesWindow", y: int, x: int, text: str, attr: int = 0) -> None:
    """addstr that swallows out-of-bounds errors on small terminals."""
    max_y, max_x = win.getmaxyx()
    if y < 0 or y >= max_y or x < 0 or x >= max_x:
        return
    if x + len(text) > max_x:
        text = text[: max(0, max_x - x - 1)]
    try:
        win.addstr(y, x, text, attr)
    except curses.error:
        pass


def _draw_header(win: "curses._CursesWindow", title: str, subtitle: str = "") -> None:
    max_y, max_x = win.getmaxyx()
    bar = (" " + title + (" — " + subtitle if subtitle else "")).ljust(max_x - 1)
    _safe_addstr(win, 0, 0, bar, curses.A_REVERSE | curses.A_BOLD)


def _draw_status(win: "curses._CursesWindow", status: Status, hint: str) -> None:
    max_y, max_x = win.getmaxyx()
    line = max_y - 1
    del max_y  # only needed to compute `line`
    win.move(line, 0)
    win.clrtoeol()
    if status.text:
        attr = curses.A_BOLD
        if status.is_error and curses.has_colors():
            attr |= curses.color_pair(1)
        _safe_addstr(win, line, 0, status.text[: max_x - 1], attr)
    else:
        _safe_addstr(win, line, 0, hint[: max_x - 1], curses.A_DIM)


def _prompt(stdscr: "curses._CursesWindow", label: str, initial: str = "") -> str | None:
    """Inline single-line prompt at the bottom row. Returns None on Esc."""
    max_y, max_x = stdscr.getmaxyx()
    line = max_y - 1
    stdscr.move(line, 0)
    stdscr.clrtoeol()
    _safe_addstr(stdscr, line, 0, label, curses.A_BOLD)
    curses.curs_set(1)
    curses.echo()
    try:
        # Pre-seed the input field by drawing initial text and using getstr.
        prefix = label
        if initial:
            _safe_addstr(stdscr, line, len(prefix), initial)
            stdscr.move(line, len(prefix) + len(initial))
        try:
            raw = stdscr.getstr(line, len(prefix), max_x - len(prefix) - 1)
        except KeyboardInterrupt:
            return None
        if raw is None:
            return None
        text = raw.decode(
            "utf-8", errors="replace") if isinstance(raw, bytes) else str(raw)
        # If user provided initial text and pressed Enter without editing,
        # getstr returns empty — treat empty as "keep initial".
        if not text and initial:
            return initial
        return text
    finally:
        curses.noecho()
        curses.curs_set(0)


def _confirm(stdscr: "curses._CursesWindow", question: str) -> bool:
    ans = _prompt(stdscr, question + " [y/N]: ")
    return (ans or "").strip().lower() in {"y", "yes"}


def _shell_edit(initial_text: str, suffix: str = ".json") -> str | None:
    """Run $EDITOR on a temp file pre-populated with initial_text.

    Curses is suspended for the duration. Returns the edited text or None
    if the editor exited non-zero.
    """
    editor = os.environ.get("EDITOR") or os.environ.get("VISUAL") or "vi"
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=suffix, delete=False, encoding="utf-8"
    ) as tmp:
        tmp.write(initial_text)
        if not initial_text.endswith("\n"):
            tmp.write("\n")
        path = tmp.name
    try:
        curses.endwin()
        proc = subprocess.run(shlex.split(editor) + [path], check=False)
        if proc.returncode != 0:
            return None
        with open(path, encoding="utf-8") as f:
            return f.read()
    finally:
        try:
            os.unlink(path)
        except OSError:
            pass


# ---------------------------------------------------------------------------
# Scrollable list helper (shared by indices/docs screens)
# ---------------------------------------------------------------------------


@dataclass
class ListState:
    items: list[Any] = field(default_factory=list)
    cursor: int = 0
    top: int = 0

    def move(self, delta: int, viewport: int) -> None:
        if not self.items:
            self.cursor = 0
            self.top = 0
            return
        self.cursor = max(0, min(len(self.items) - 1, self.cursor + delta))
        if self.cursor < self.top:
            self.top = self.cursor
        elif self.cursor >= self.top + viewport:
            self.top = self.cursor - viewport + 1

    def home(self) -> None:
        self.cursor = 0
        self.top = 0

    def end(self, viewport: int) -> None:
        if not self.items:
            return
        self.cursor = len(self.items) - 1
        self.top = max(0, self.cursor - viewport + 1)


def _draw_list(
    win: "curses._CursesWindow",
    top_y: int,
    height: int,
    width: int,
    state: ListState,
    render: Callable[[Any], str],
) -> None:
    for i in range(height):
        idx = state.top + i
        row_y = top_y + i
        win.move(row_y, 0)
        win.clrtoeol()
        if idx >= len(state.items):
            continue
        text = render(state.items[idx])
        attr = curses.A_REVERSE if idx == state.cursor else 0
        _safe_addstr(win, row_y, 0, text.ljust(width)[: width - 1], attr)


# ---------------------------------------------------------------------------
# Screens
# ---------------------------------------------------------------------------


@dataclass
class DocsContext:
    index: str
    query: str = ""        # Lucene q= filter
    filter_text: str = ""  # local substring filter on rendered rows
    size: int = 50
    from_: int = 0
    total: int = 0


def _fetch_indices(client: EsClient) -> list[dict[str, Any]]:
    status, body = client.request(
        "GET", "/_cat/indices", params={"format": "json", "v": "true"}
    )
    if status >= 300 or not isinstance(body, list):
        raise RuntimeError(f"_cat/indices failed: HTTP {status}: {body!r}")
    # Hide hidden/system indices (starting with .) by default
    visible = [r for r in body if not str(r.get("index", "")).startswith(".")]
    return sorted(visible, key=lambda r: r.get("index", ""))


def _fetch_docs(client: EsClient, ctx: DocsContext) -> list[dict[str, Any]]:
    params: dict[str, Any] = {"size": ctx.size, "from": ctx.from_}
    if ctx.query:
        params["q"] = ctx.query
    status, body = client.request(
        "GET", f"/{ctx.index}/_search", params=params)
    if status >= 300 or not isinstance(body, dict):
        raise RuntimeError(f"search failed: HTTP {status}: {body!r}")
    hits_block = body.get("hits", {}) or {}
    total_block = hits_block.get("total", 0)
    if isinstance(total_block, dict):
        ctx.total = int(total_block.get("value", 0))
    else:
        ctx.total = int(total_block or 0)
    return list(hits_block.get("hits", []) or [])


def _render_doc_row(hit: dict[str, Any]) -> str:
    doc_id = str(hit.get("_id", ""))
    src = hit.get("_source", {}) or {}
    preview_keys = ("name", "title", "status", "state", "@timestamp", "id")
    parts: list[str] = []
    for k in preview_keys:
        if k in src:
            v = src[k]
            if isinstance(v, (dict, list)):
                v = json.dumps(v, ensure_ascii=False)[:40]
            parts.append(f"{k}={v}")
        if len(parts) >= 3:
            break
    suffix = "  ".join(parts) if parts else json.dumps(
        src, ensure_ascii=False)[:120]
    return f"{doc_id:<40}  {suffix}"


def _filter_items(items: list[Any], needle: str, key: Callable[[Any], str]) -> list[Any]:
    if not needle:
        return items
    needle_low = needle.lower()
    return [it for it in items if needle_low in key(it).lower()]


def _indices_screen(
    stdscr: "curses._CursesWindow", client: EsClient, status: Status
) -> str | None:
    """Returns selected index name, or None to quit."""
    raw_items: list[dict[str, Any]] = []
    state = ListState()
    filter_text = ""

    def reload() -> None:
        nonlocal raw_items
        try:
            raw_items = _fetch_indices(client)
            status.clear()
        except Exception as e:  # noqa: BLE001 - surface any failure in the status bar
            status.set(f"failed to fetch indices: {e}", error=True)
            raw_items = []
        apply_filter()

    def apply_filter() -> None:
        state.items = _filter_items(
            raw_items, filter_text, lambda r: r.get("index", ""))
        state.cursor = min(state.cursor, max(0, len(state.items) - 1))
        state.top = min(state.top, state.cursor)

    reload()

    while True:
        stdscr.erase()
        max_y, max_x = stdscr.getmaxyx()
        _draw_header(
            stdscr,
            "Indices",
            f"{client.base_url}   ({len(state.items)} shown / {len(raw_items)} total)"
            + (f"   filter: {filter_text!r}" if filter_text else ""),
        )

        col_hdr = f"{'health':<6}  {'index':<40}  {'docs':>10}  {'size':>10}"
        _safe_addstr(stdscr, 2, 0, col_hdr, curses.A_UNDERLINE)

        body_top = 3
        body_height = max_y - body_top - 1

        def render(r: dict[str, Any]) -> str:
            return "{:<6}  {:<40}  {:>10}  {:>10}".format(
                str(r.get("health", "")),
                str(r.get("index", ""))[:40],
                str(r.get("docs.count", "")),
                str(r.get("store.size", "")),
            )

        _draw_list(stdscr, body_top, body_height, max_x, state, render)
        _draw_status(
            stdscr,
            status,
            "↑/↓ move  Enter open  / filter  r refresh  q quit",
        )
        stdscr.refresh()

        ch = stdscr.getch()
        if ch in (ord("q"), 27):  # q or Esc
            return None
        elif ch in (curses.KEY_UP, ord("k")):
            state.move(-1, body_height)
        elif ch in (curses.KEY_DOWN, ord("j")):
            state.move(1, body_height)
        elif ch == curses.KEY_NPAGE:
            state.move(body_height, body_height)
        elif ch == curses.KEY_PPAGE:
            state.move(-body_height, body_height)
        elif ch in (curses.KEY_HOME, ord("g")):
            state.home()
        elif ch in (curses.KEY_END, ord("G")):
            state.end(body_height)
        elif ch in (curses.KEY_ENTER, 10, 13, ord("\n"), ord("v")):
            if state.items:
                return state.items[state.cursor].get("index")
        elif ch == ord("/"):
            new = _prompt(stdscr, "filter: ", filter_text)
            if new is not None:
                filter_text = new
                apply_filter()
        elif ch == ord("r"):
            reload()


def _docs_screen(
    stdscr: "curses._CursesWindow", client: EsClient, ctx: DocsContext, status: Status
) -> None:
    raw_hits: list[dict[str, Any]] = []
    state = ListState()

    def reload() -> None:
        nonlocal raw_hits
        try:
            raw_hits = _fetch_docs(client, ctx)
            status.clear()
        except Exception as e:  # noqa: BLE001 - surface any failure in the status bar
            status.set(f"search failed: {e}", error=True)
            raw_hits = []
        apply_filter()

    def apply_filter() -> None:
        state.items = _filter_items(raw_hits, ctx.filter_text, _render_doc_row)
        state.cursor = min(state.cursor, max(0, len(state.items) - 1))
        state.top = min(state.top, state.cursor)

    reload()

    while True:
        stdscr.erase()
        max_y, max_x = stdscr.getmaxyx()

        subtitle_parts = [
            f"q={ctx.query!r}" if ctx.query else "all",
            f"page from={ctx.from_} size={ctx.size}",
            f"shown={len(state.items)}/{len(raw_hits)} total={ctx.total}",
        ]
        if ctx.filter_text:
            subtitle_parts.append(f"filter={ctx.filter_text!r}")
        _draw_header(stdscr, f"{ctx.index}", "   ".join(subtitle_parts))

        _safe_addstr(stdscr, 2, 0, f"{'_id':<40}  preview", curses.A_UNDERLINE)

        body_top = 3
        body_height = max_y - body_top - 1
        _draw_list(stdscr, body_top, body_height,
                   max_x, state, _render_doc_row)

        _draw_status(
            stdscr,
            status,
            "Enter/v view  e edit  d delete  / filter  f query  n/p page  s size  r refresh  b back  q quit",
        )
        stdscr.refresh()

        ch = stdscr.getch()
        if ch == ord("q"):
            raise _QuitRequested
        if ch in (ord("b"), 27):
            return
        elif ch in (curses.KEY_UP, ord("k")):
            state.move(-1, body_height)
        elif ch in (curses.KEY_DOWN, ord("j")):
            state.move(1, body_height)
        elif ch == curses.KEY_NPAGE:
            state.move(body_height, body_height)
        elif ch == curses.KEY_PPAGE:
            state.move(-body_height, body_height)
        elif ch in (curses.KEY_HOME, ord("g")):
            state.home()
        elif ch in (curses.KEY_END, ord("G")):
            state.end(body_height)
        elif ch in (curses.KEY_ENTER, 10, 13, ord("\n"), ord("v")):
            if state.items:
                _view_doc_screen(stdscr, client, ctx.index,
                                 state.items[state.cursor], status)
                reload()
        elif ch == ord("e"):
            if state.items:
                _edit_doc(stdscr, client, ctx.index,
                          state.items[state.cursor], status)
                reload()
        elif ch == ord("d"):
            if state.items:
                _delete_doc(stdscr, client, ctx.index,
                            state.items[state.cursor], status)
                reload()
        elif ch == ord("/"):
            new = _prompt(
                stdscr, "filter (id/source substring): ", ctx.filter_text)
            if new is not None:
                ctx.filter_text = new
                apply_filter()
        elif ch == ord("f"):
            new = _prompt(stdscr, "Lucene q (empty = all): ", ctx.query)
            if new is not None:
                ctx.query = new
                ctx.from_ = 0
                state.home()
                reload()
        elif ch == ord("n"):
            ctx.from_ += ctx.size
            state.home()
            reload()
        elif ch == ord("p"):
            ctx.from_ = max(0, ctx.from_ - ctx.size)
            state.home()
            reload()
        elif ch == ord("s"):
            new = _prompt(stdscr, "page size: ", str(ctx.size))
            if new and new.strip().isdigit():
                ctx.size = max(1, min(10000, int(new.strip())))
                ctx.from_ = 0
                state.home()
                reload()
        elif ch == ord("r"):
            reload()


def _view_doc_screen(
    stdscr: "curses._CursesWindow",
    client: EsClient,
    index: str,
    hit: dict[str, Any],
    status: Status,
) -> None:
    doc_id = str(hit.get("_id", ""))
    source = hit.get("_source", {}) or {}
    text = json.dumps(source, indent=2, sort_keys=True, ensure_ascii=False)
    lines = text.splitlines() or [""]
    top = 0

    while True:
        stdscr.erase()
        max_y, max_x = stdscr.getmaxyx()
        _draw_header(stdscr, f"{index} / {doc_id}", f"{len(lines)} lines")
        body_top = 2
        body_height = max_y - body_top - 1
        top = max(0, min(top, max(0, len(lines) - body_height)))
        for i in range(body_height):
            li = top + i
            if li >= len(lines):
                break
            _safe_addstr(stdscr, body_top + i, 0, lines[li][: max_x - 1])
        _draw_status(
            stdscr,
            status,
            "↑/↓ scroll  PgUp/PgDn page  g/G top/bot  e edit  d delete  b/Esc back  q quit",
        )
        stdscr.refresh()

        ch = stdscr.getch()
        if ch == ord("q"):
            raise _QuitRequested
        if ch in (ord("b"), 27):
            return
        elif ch in (curses.KEY_UP, ord("k")):
            top -= 1
        elif ch in (curses.KEY_DOWN, ord("j")):
            top += 1
        elif ch == curses.KEY_NPAGE:
            top += body_height
        elif ch == curses.KEY_PPAGE:
            top -= body_height
        elif ch in (curses.KEY_HOME, ord("g")):
            top = 0
        elif ch in (curses.KEY_END, ord("G")):
            top = max(0, len(lines) - body_height)
        elif ch == ord("e"):
            if _edit_doc(stdscr, client, index, hit, status):
                # reload fresh copy after successful edit
                s, body = client.request("GET", f"/{index}/_doc/{doc_id}")
                if s < 300 and isinstance(body, dict):
                    source = body.get("_source", {}) or {}
                    text = json.dumps(source, indent=2,
                                      sort_keys=True, ensure_ascii=False)
                    lines = text.splitlines() or [""]
                    # refresh hit dict so subsequent edits use new _seq_no
                    hit.update(body)
        elif ch == ord("d"):
            if _delete_doc(stdscr, client, index, hit, status):
                return


# ---------------------------------------------------------------------------
# Mutating actions
# ---------------------------------------------------------------------------


def _edit_doc(
    stdscr: "curses._CursesWindow",
    client: EsClient,
    index: str,
    hit: dict[str, Any],
    status: Status,
) -> bool:
    """Open the doc in $EDITOR and PUT it back. Returns True on success."""
    doc_id = str(hit.get("_id", ""))
    # fetch latest so _seq_no/_primary_term are fresh
    s, body = client.request("GET", f"/{index}/_doc/{doc_id}")
    if s >= 300 or not isinstance(body, dict):
        status.set(f"get failed: HTTP {s}", error=True)
        return False
    source = body.get("_source", {}) or {}
    original_text = json.dumps(
        source, indent=2, sort_keys=True, ensure_ascii=False)

    edited_text = _shell_edit(original_text)
    if edited_text is None:
        status.set("edit cancelled", error=True)
        return False
    if edited_text.strip() == original_text.strip():
        status.set("no changes")
        return False
    try:
        new_source = json.loads(edited_text)
    except json.JSONDecodeError as e:
        status.set(f"invalid JSON: {e}", error=True)
        return False

    params: dict[str, Any] = {"refresh": "true"}
    if "_seq_no" in body and "_primary_term" in body:
        params["if_seq_no"] = body["_seq_no"]
        params["if_primary_term"] = body["_primary_term"]
    s, resp = client.request(
        "PUT", f"/{index}/_doc/{doc_id}", body=new_source, params=params
    )
    if s >= 300:
        status.set(f"update failed: HTTP {s}: {resp!r}"[:200], error=True)
        return False
    status.set(
        f"updated {index}/{doc_id} ({resp.get('result') if isinstance(resp, dict) else 'ok'})")
    # keep caller's hit roughly in sync
    hit["_source"] = new_source
    return True


def _delete_doc(
    stdscr: "curses._CursesWindow",
    client: EsClient,
    index: str,
    hit: dict[str, Any],
    status: Status,
) -> bool:
    doc_id = str(hit.get("_id", ""))
    _ = stdscr  # only used implicitly via _confirm's curses I/O
    if not _confirm(stdscr, f"delete {index}/{doc_id}?"):
        status.set("delete cancelled")
        return False
    s, resp = client.request(
        "DELETE", f"/{index}/_doc/{doc_id}", params={"refresh": "true"}
    )
    if s >= 300:
        status.set(f"delete failed: HTTP {s}: {resp!r}"[:200], error=True)
        return False
    status.set(f"deleted {index}/{doc_id}")
    return True


# ---------------------------------------------------------------------------
# Entrypoint
# ---------------------------------------------------------------------------


class _QuitRequested(Exception):
    """Internal control-flow exception to unwind nested screens on 'q'."""


def _curses_main(stdscr: "curses._CursesWindow", client: EsClient, start_index: str | None) -> None:
    curses.curs_set(0)
    if curses.has_colors():
        curses.start_color()
        curses.use_default_colors()
        curses.init_pair(1, curses.COLOR_RED, -1)
    stdscr.keypad(True)

    status = Status()
    try:
        if start_index:
            ctx = DocsContext(index=start_index)
            _docs_screen(stdscr, client, ctx, status)
            return
        while True:
            chosen = _indices_screen(stdscr, client, status)
            if chosen is None:
                return
            ctx = DocsContext(index=chosen)
            _docs_screen(stdscr, client, ctx, status)
    except _QuitRequested:
        return


def run_tui(client: EsClient, start_index: str | None = None) -> None:
    """Public entrypoint used by es_tool's CLI."""
    curses.wrapper(_curses_main, client, start_index)
