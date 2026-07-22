// Package tui implements the full-screen interactive Elasticsearch browser
// (indices → docs → viewer) built on tcell.
package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

// ---------------------------------------------------------------------------
// App / status
// ---------------------------------------------------------------------------

type app struct {
	screen tcell.Screen
	client *esclient.Client
	status statusMsg
}

type statusMsg struct {
	text  string
	isErr bool
}

func (s *statusMsg) set(text string)    { s.text, s.isErr = text, false }
func (s *statusMsg) setErr(text string) { s.text, s.isErr = text, true }
func (s *statusMsg) clear()             { s.text, s.isErr = "", false }

// quitSignal unwinds nested screens when the user presses 'q'.
type quitSignal struct{}

// Run is the public entrypoint used by the CLI.
func Run(client *esclient.Client, startIndex string) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("tui unavailable: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("tui init failed: %w", err)
	}
	defer screen.Fini()
	screen.HideCursor()

	a := &app{screen: screen, client: client}

	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(quitSignal); ok {
				return
			}
			panic(r)
		}
	}()

	if startIndex != "" {
		a.docsScreen(&docsContext{index: startIndex, size: 50})
		return nil
	}
	for {
		chosen, quit := a.indicesScreen()
		if quit {
			return nil
		}
		a.docsScreen(&docsContext{index: chosen, size: 50})
	}
}

// ---------------------------------------------------------------------------
// Styles & drawing primitives
// ---------------------------------------------------------------------------

var (
	styleDefault = tcell.StyleDefault
	styleHeader  = tcell.StyleDefault.Reverse(true).Bold(true)
	styleSelect  = tcell.StyleDefault.Reverse(true)
	styleDim     = tcell.StyleDefault.Dim(true)
	styleUnder   = tcell.StyleDefault.Underline(true)
	styleErr     = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleBold    = tcell.StyleDefault.Bold(true)
)

func (a *app) size() (int, int) { return a.screen.Size() }

// drawText writes s at (x,y) clipped to the screen width.
func (a *app) drawText(x, y int, style tcell.Style, s string) {
	maxX, maxY := a.screen.Size()
	if y < 0 || y >= maxY {
		return
	}
	for _, r := range s {
		if x >= maxX {
			break
		}
		if x >= 0 {
			a.screen.SetContent(x, y, r, nil, style)
		}
		x++
	}
}

// drawBar fills row y with s padded to full width using style.
func (a *app) drawBar(y int, style tcell.Style, s string) {
	maxX, _ := a.screen.Size()
	a.drawText(0, y, style, util.PadRight(util.Clip(s, maxX), maxX))
}

func (a *app) clearLine(y int) {
	maxX, _ := a.screen.Size()
	a.drawText(0, y, styleDefault, strings.Repeat(" ", maxX))
}

func (a *app) drawHeader(title, subtitle string) {
	bar := " " + title
	if subtitle != "" {
		bar += " — " + subtitle
	}
	a.drawBar(0, styleHeader, bar)
}

func (a *app) drawStatus(hint string) {
	_, maxY := a.size()
	line := maxY - 1
	a.clearLine(line)
	if a.status.text != "" {
		st := styleBold
		if a.status.isErr {
			st = styleErr
		}
		a.drawText(0, line, st, a.status.text)
	} else {
		a.drawText(0, line, styleDim, hint)
	}
}

// ---------------------------------------------------------------------------
// Prompt / confirm
// ---------------------------------------------------------------------------

// prompt shows an inline single-line editor at the bottom row. It returns the
// entered text and ok=false if the user pressed Esc.
func (a *app) prompt(label, initial string) (string, bool) {
	_, maxY := a.size()
	line := maxY - 1
	buf := []rune(initial)

	for {
		a.clearLine(line)
		a.drawText(0, line, styleBold, label)
		a.drawText(len(label), line, styleDefault, string(buf))
		a.screen.ShowCursor(len(label)+len(buf), line)
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch ke.Key() {
		case tcell.KeyEscape:
			a.screen.HideCursor()
			return "", false
		case tcell.KeyEnter:
			a.screen.HideCursor()
			return string(buf), true
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
			}
		case tcell.KeyRune:
			buf = append(buf, ke.Rune())
		}
	}
}

func (a *app) confirm(question string) bool {
	ans, ok := a.prompt(question+" [y/N]: ", "")
	if !ok {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}

// ---------------------------------------------------------------------------
// List state (shared by indices/docs screens)
// ---------------------------------------------------------------------------

type listState struct {
	length int
	cursor int
	top    int
}

func (l *listState) move(delta, viewport int) {
	if l.length == 0 {
		l.cursor, l.top = 0, 0
		return
	}
	l.cursor = max(0, min(l.length-1, l.cursor+delta))
	if l.cursor < l.top {
		l.top = l.cursor
	} else if l.cursor >= l.top+viewport {
		l.top = l.cursor - viewport + 1
	}
}

func (l *listState) home() { l.cursor, l.top = 0, 0 }

func (l *listState) end(viewport int) {
	if l.length == 0 {
		return
	}
	l.cursor = l.length - 1
	l.top = max(0, l.cursor-viewport+1)
}

func (a *app) drawList(topY, height, width int, st *listState, render func(i int) string) {
	for i := 0; i < height; i++ {
		idx := st.top + i
		rowY := topY + i
		a.clearLine(rowY)
		if idx >= st.length {
			continue
		}
		style := styleDefault
		if idx == st.cursor {
			style = styleSelect
		}
		a.drawText(0, rowY, style, util.Clip(util.PadRight(render(idx), width), width))
	}
}

// ---------------------------------------------------------------------------
// Data fetching
// ---------------------------------------------------------------------------

type docsContext struct {
	index      string
	query      string
	filterText string
	size       int
	from       int
	total      int
}

func (a *app) fetchIndices() ([]map[string]any, error) {
	status, body, err := a.client.Request("GET", "/_cat/indices",
		nil, map[string]string{"format": "json", "v": "true"})
	if err != nil {
		return nil, err
	}
	arr, ok := body.([]any)
	if status >= 300 || !ok {
		return nil, fmt.Errorf("_cat/indices failed: HTTP %d", status)
	}
	var visible []map[string]any
	for _, r := range arr {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if strings.HasPrefix(util.AsStr(m["index"]), ".") {
			continue
		}
		visible = append(visible, m)
	}
	sort.Slice(visible, func(i, j int) bool {
		return util.AsStr(visible[i]["index"]) < util.AsStr(visible[j]["index"])
	})
	return visible, nil
}

func (a *app) fetchDocs(ctx *docsContext) ([]map[string]any, error) {
	params := map[string]string{
		"size": strconv.Itoa(ctx.size),
		"from": strconv.Itoa(ctx.from),
	}
	if ctx.query != "" {
		params["q"] = ctx.query
	}
	status, body, err := a.client.Request("GET", "/"+ctx.index+"/_search", nil, params)
	if err != nil {
		return nil, err
	}
	m, ok := body.(map[string]any)
	if status >= 300 || !ok {
		return nil, fmt.Errorf("search failed: HTTP %d", status)
	}
	hitsBlock, _ := m["hits"].(map[string]any)
	switch t := hitsBlock["total"].(type) {
	case map[string]any:
		ctx.total = util.AsInt(t["value"])
	default:
		ctx.total = util.AsInt(hitsBlock["total"])
	}
	var hits []map[string]any
	if arr, ok := hitsBlock["hits"].([]any); ok {
		for _, h := range arr {
			if hm, ok := h.(map[string]any); ok {
				hits = append(hits, hm)
			}
		}
	}
	return hits, nil
}

func renderDocRow(hit map[string]any) string {
	docID := util.AsStr(hit["_id"])
	src, _ := hit["_source"].(map[string]any)
	previewKeys := []string{"name", "title", "status", "state", "@timestamp", "id"}
	var parts []string
	for _, k := range previewKeys {
		if v, ok := src[k]; ok {
			var vs string
			switch v.(type) {
			case map[string]any, []any:
				b, _ := json.Marshal(v)
				vs = util.Clip(string(b), 40)
			default:
				vs = util.AsStr(v)
			}
			parts = append(parts, fmt.Sprintf("%s=%s", k, vs))
		}
		if len(parts) >= 3 {
			break
		}
	}
	var suffix string
	if len(parts) > 0 {
		suffix = strings.Join(parts, "  ")
	} else {
		b, _ := json.Marshal(src)
		suffix = util.Clip(string(b), 120)
	}
	return fmt.Sprintf("%-40s  %s", docID, suffix)
}

func filterIndices(items []map[string]any, needle string) []map[string]any {
	if needle == "" {
		return items
	}
	low := strings.ToLower(needle)
	var out []map[string]any
	for _, it := range items {
		if strings.Contains(strings.ToLower(util.AsStr(it["index"])), low) {
			out = append(out, it)
		}
	}
	return out
}

func filterHits(items []map[string]any, needle string) []map[string]any {
	if needle == "" {
		return items
	}
	low := strings.ToLower(needle)
	var out []map[string]any
	for _, it := range items {
		if strings.Contains(strings.ToLower(renderDocRow(it)), low) {
			out = append(out, it)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Indices screen
// ---------------------------------------------------------------------------

func (a *app) indicesScreen() (selected string, quit bool) {
	var rawItems, items []map[string]any
	st := &listState{}
	filterText := ""

	applyFilter := func() {
		items = filterIndices(rawItems, filterText)
		st.length = len(items)
		st.cursor = min(st.cursor, max(0, st.length-1))
		st.top = min(st.top, st.cursor)
	}
	reload := func() {
		v, err := a.fetchIndices()
		if err != nil {
			a.status.setErr("failed to fetch indices: " + err.Error())
			rawItems = nil
		} else {
			a.status.clear()
			rawItems = v
		}
		applyFilter()
	}
	reload()

	for {
		a.screen.Clear()
		maxX, maxY := a.size()
		sub := fmt.Sprintf("%s   (%d shown / %d total)", a.client.BaseURL, len(items), len(rawItems))
		if filterText != "" {
			sub += fmt.Sprintf("   filter: %q", filterText)
		}
		a.drawHeader("Indices", sub)

		colHdr := fmt.Sprintf("%-6s  %-40s  %10s  %10s", "health", "index", "docs", "size")
		a.drawText(0, 2, styleUnder, util.Clip(colHdr, maxX))

		bodyTop := 3
		bodyHeight := maxY - bodyTop - 1
		a.drawList(bodyTop, bodyHeight, maxX, st, func(i int) string {
			r := items[i]
			return fmt.Sprintf("%-6s  %-40s  %10s  %10s",
				util.AsStr(r["health"]),
				util.Clip(util.AsStr(r["index"]), 40),
				util.AsStr(r["docs.count"]),
				util.AsStr(r["store.size"]))
		})
		a.drawStatus("↑/↓ move  Enter open  / filter  r refresh  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q', ke.Key() == tcell.KeyEscape:
			return "", true
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			st.move(-1, bodyHeight)
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			st.move(1, bodyHeight)
		case ke.Key() == tcell.KeyPgDn:
			st.move(bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyPgUp:
			st.move(-bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			st.home()
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			st.end(bodyHeight)
		case ke.Key() == tcell.KeyEnter, ke.Key() == tcell.KeyRune && ke.Rune() == 'v':
			if st.length > 0 {
				return util.AsStr(items[st.cursor]["index"]), false
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == '/':
			if v, ok := a.prompt("filter: ", filterText); ok {
				filterText = v
				applyFilter()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'r':
			reload()
		}
	}
}

// ---------------------------------------------------------------------------
// Docs screen
// ---------------------------------------------------------------------------

func (a *app) docsScreen(ctx *docsContext) {
	var rawHits, items []map[string]any
	st := &listState{}

	applyFilter := func() {
		items = filterHits(rawHits, ctx.filterText)
		st.length = len(items)
		st.cursor = min(st.cursor, max(0, st.length-1))
		st.top = min(st.top, st.cursor)
	}
	reload := func() {
		v, err := a.fetchDocs(ctx)
		if err != nil {
			a.status.setErr("search failed: " + err.Error())
			rawHits = nil
		} else {
			a.status.clear()
			rawHits = v
		}
		applyFilter()
	}
	reload()

	for {
		a.screen.Clear()
		maxX, maxY := a.size()

		queryPart := "all"
		if ctx.query != "" {
			queryPart = fmt.Sprintf("q=%q", ctx.query)
		}
		subParts := []string{
			queryPart,
			fmt.Sprintf("page from=%d size=%d", ctx.from, ctx.size),
			fmt.Sprintf("shown=%d/%d total=%d", len(items), len(rawHits), ctx.total),
		}
		if ctx.filterText != "" {
			subParts = append(subParts, fmt.Sprintf("filter=%q", ctx.filterText))
		}
		a.drawHeader(ctx.index, strings.Join(subParts, "   "))
		a.drawText(0, 2, styleUnder, util.Clip(fmt.Sprintf("%-40s  preview", "_id"), maxX))

		bodyTop := 3
		bodyHeight := maxY - bodyTop - 1
		a.drawList(bodyTop, bodyHeight, maxX, st, func(i int) string {
			return renderDocRow(items[i])
		})
		a.drawStatus("Enter/v view  e edit  d delete  / filter  f query  n/p page  s size  r refresh  b back  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q':
			panic(quitSignal{})
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'b', ke.Key() == tcell.KeyEscape:
			return
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			st.move(-1, bodyHeight)
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			st.move(1, bodyHeight)
		case ke.Key() == tcell.KeyPgDn:
			st.move(bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyPgUp:
			st.move(-bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			st.home()
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			st.end(bodyHeight)
		case ke.Key() == tcell.KeyEnter, ke.Key() == tcell.KeyRune && ke.Rune() == 'v':
			if st.length > 0 {
				a.viewDocScreen(ctx.index, items[st.cursor])
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'e':
			if st.length > 0 {
				a.editDoc(ctx.index, items[st.cursor])
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'd':
			if st.length > 0 {
				a.deleteDoc(ctx.index, items[st.cursor])
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == '/':
			if v, ok := a.prompt("filter (id/source substring): ", ctx.filterText); ok {
				ctx.filterText = v
				applyFilter()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'f':
			if v, ok := a.prompt("Lucene q (empty = all): ", ctx.query); ok {
				ctx.query = v
				ctx.from = 0
				st.home()
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'n':
			ctx.from += ctx.size
			st.home()
			reload()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'p':
			ctx.from = max(0, ctx.from-ctx.size)
			st.home()
			reload()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 's':
			if v, ok := a.prompt("page size: ", strconv.Itoa(ctx.size)); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					ctx.size = max(1, min(10000, n))
					ctx.from = 0
					st.home()
					reload()
				}
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'r':
			reload()
		}
	}
}

// ---------------------------------------------------------------------------
// Viewer screen
// ---------------------------------------------------------------------------

func (a *app) viewDocScreen(index string, hit map[string]any) {
	docID := util.AsStr(hit["_id"])
	source := hit["_source"]
	text, _ := util.MarshalIndent(source)
	lines := splitLines(text)
	top := 0

	for {
		a.screen.Clear()
		maxX, maxY := a.size()
		a.drawHeader(index+" / "+docID, fmt.Sprintf("%d lines", len(lines)))
		bodyTop := 2
		bodyHeight := maxY - bodyTop - 1
		top = max(0, min(top, max(0, len(lines)-bodyHeight)))
		for i := 0; i < bodyHeight; i++ {
			li := top + i
			if li >= len(lines) {
				break
			}
			a.drawText(0, bodyTop+i, styleDefault, util.Clip(lines[li], maxX))
		}
		a.drawStatus("↑/↓ scroll  PgUp/PgDn page  g/G top/bot  e edit  d delete  b/Esc back  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q':
			panic(quitSignal{})
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'b', ke.Key() == tcell.KeyEscape:
			return
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			top--
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			top++
		case ke.Key() == tcell.KeyPgDn:
			top += bodyHeight
		case ke.Key() == tcell.KeyPgUp:
			top -= bodyHeight
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			top = 0
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			top = max(0, len(lines)-bodyHeight)
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'e':
			if a.editDoc(index, hit) {
				s, body, _ := a.client.Request("GET", "/"+index+"/_doc/"+docID, nil, nil)
				if s < 300 {
					if m, ok := body.(map[string]any); ok {
						source = m["_source"]
						text, _ = util.MarshalIndent(source)
						lines = splitLines(text)
						for k, v := range m {
							hit[k] = v
						}
					}
				}
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'd':
			if a.deleteDoc(index, hit) {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Mutating actions
// ---------------------------------------------------------------------------

// editDoc fetches the latest doc, opens it in $EDITOR (tcell suspended), and
// PUTs it back with optimistic concurrency control. Returns true on success.
func (a *app) editDoc(index string, hit map[string]any) bool {
	docID := util.AsStr(hit["_id"])
	s, body, err := a.client.Request("GET", "/"+index+"/_doc/"+docID, nil, nil)
	if err != nil || s >= 300 {
		a.status.setErr(fmt.Sprintf("get failed: HTTP %d", s))
		return false
	}
	m, ok := body.(map[string]any)
	if !ok {
		a.status.setErr("get failed: unexpected response")
		return false
	}
	source := m["_source"]
	originalText, _ := util.MarshalIndent(source)

	editedText, ok := a.suspendEdit(originalText)
	if !ok {
		a.status.setErr("edit cancelled")
		return false
	}
	if strings.TrimSpace(editedText) == strings.TrimSpace(originalText) {
		a.status.set("no changes")
		return false
	}
	var newSource any
	dec := json.NewDecoder(strings.NewReader(editedText))
	dec.UseNumber()
	if err := dec.Decode(&newSource); err != nil {
		a.status.setErr("invalid JSON: " + err.Error())
		return false
	}

	params := map[string]string{"refresh": "true"}
	if seq, ok1 := m["_seq_no"]; ok1 {
		if term, ok2 := m["_primary_term"]; ok2 {
			params["if_seq_no"] = util.AsStr(seq)
			params["if_primary_term"] = util.AsStr(term)
		}
	}
	s, resp, err := a.client.Request("PUT", "/"+index+"/_doc/"+docID, newSource, params)
	if err != nil || s >= 300 {
		a.status.setErr(util.Clip(fmt.Sprintf("update failed: HTTP %d: %v", s, resp), 200))
		return false
	}
	result := "ok"
	if rm, ok := resp.(map[string]any); ok {
		if r := util.AsStr(rm["result"]); r != "" {
			result = r
		}
	}
	a.status.set(fmt.Sprintf("updated %s/%s (%s)", index, docID, result))
	hit["_source"] = newSource
	return true
}

func (a *app) deleteDoc(index string, hit map[string]any) bool {
	docID := util.AsStr(hit["_id"])
	if !a.confirm(fmt.Sprintf("delete %s/%s?", index, docID)) {
		a.status.set("delete cancelled")
		return false
	}
	s, resp, err := a.client.Request("DELETE", "/"+index+"/_doc/"+docID, nil, map[string]string{"refresh": "true"})
	if err != nil || s >= 300 {
		a.status.setErr(util.Clip(fmt.Sprintf("delete failed: HTTP %d: %v", s, resp), 200))
		return false
	}
	a.status.set(fmt.Sprintf("deleted %s/%s", index, docID))
	return true
}

// suspendEdit suspends the tcell screen, runs $EDITOR, and resumes.
func (a *app) suspendEdit(initialText string) (string, bool) {
	if err := a.screen.Suspend(); err != nil {
		a.status.setErr("cannot suspend screen: " + err.Error())
		return "", false
	}
	edited, err := util.ShellEdit(initialText+"\n", ".json")
	_ = a.screen.Resume()
	if err != nil {
		return "", false
	}
	return edited, true
}

func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
