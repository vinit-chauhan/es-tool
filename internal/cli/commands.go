package cli

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/tui"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

func cmdPing(client *esclient.Client, _ []string) error {
	status, body, err := client.Request("GET", "/", nil, nil)
	if err != nil {
		return err
	}
	if body, err = mustOK(status, body); err != nil {
		return err
	}
	printJSON(body)
	return nil
}

func cmdIndices(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("indices", flag.ContinueOnError)
	if err := parseFlags(fs, args, nil); err != nil {
		return err
	}
	pattern := fs.Arg(0)

	params := map[string]string{"format": "json", "v": "true"}
	path := "/_cat/indices"
	if pattern != "" {
		path = "/_cat/indices/" + pattern
	}
	status, body, err := client.Request("GET", path, nil, params)
	if err != nil {
		return err
	}
	if body, err = mustOK(status, body); err != nil {
		return err
	}

	rows, ok := body.([]any)
	if !ok {
		printJSON(body)
		return nil
	}
	printIndicesTable(rows)
	return nil
}

func printIndicesTable(rows []any) {
	cols := []string{"health", "status", "index", "docs.count", "store.size"}
	maps := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			maps = append(maps, m)
		}
	}
	sort.Slice(maps, func(i, j int) bool {
		return util.AsStr(maps[i]["index"]) < util.AsStr(maps[j]["index"])
	})

	widths := make(map[string]int, len(cols))
	for _, c := range cols {
		widths[c] = len(c)
		for _, m := range maps {
			if l := len(util.AsStr(m[c])); l > widths[c] {
				widths[c] = l
			}
		}
	}

	var header, sep strings.Builder
	for i, c := range cols {
		if i > 0 {
			header.WriteString("  ")
			sep.WriteString("  ")
		}
		header.WriteString(util.PadRight(c, widths[c]))
		sep.WriteString(strings.Repeat("-", widths[c]))
	}
	fmt.Println(header.String())
	fmt.Println(sep.String())
	for _, m := range maps {
		var line strings.Builder
		for i, c := range cols {
			if i > 0 {
				line.WriteString("  ")
			}
			line.WriteString(util.PadRight(util.AsStr(m[c]), widths[c]))
		}
		fmt.Println(line.String())
	}
}

func cmdMapping(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("mapping", flag.ContinueOnError)
	if err := parseFlags(fs, args, nil); err != nil {
		return err
	}
	index := fs.Arg(0)
	if index == "" {
		return errors.New("mapping: index required")
	}
	status, body, err := client.Request("GET", "/"+index+"/_mapping", nil, nil)
	if err != nil {
		return err
	}
	if body, err = mustOK(status, body); err != nil {
		return err
	}
	printJSON(body)
	return nil
}

func cmdCount(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("count", flag.ContinueOnError)
	q := fs.String("q", "", "Lucene query string")
	if err := parseFlags(fs, args, nil); err != nil {
		return err
	}
	index := fs.Arg(0)
	if index == "" {
		return errors.New("count: index required")
	}
	var params map[string]string
	if *q != "" {
		params = map[string]string{"q": *q}
	}
	status, body, err := client.Request("GET", "/"+index+"/_count", nil, params)
	if err != nil {
		return err
	}
	if body, err = mustOK(status, body); err != nil {
		return err
	}
	printJSON(body)
	return nil
}

func cmdSearch(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	q := fs.String("q", "", "Lucene query string")
	size := fs.Int("size", 10, "number of hits")
	sortArg := fs.String("sort", "", "sort, e.g. @timestamp:desc")
	source := fs.String("source", "", "comma-separated _source includes")
	body := fs.String("body", "", "JSON body (or @file)")
	idsOnly := fs.Bool("ids-only", false, "print only hit _ids")
	if err := parseFlags(fs, args, map[string]bool{"ids-only": true}); err != nil {
		return err
	}
	index := fs.Arg(0)
	if index == "" {
		return errors.New("search: index required")
	}

	params := map[string]string{"size": strconv.Itoa(*size)}
	if *q != "" {
		params["q"] = *q
	}
	if *sortArg != "" {
		params["sort"] = *sortArg
	}
	if *source != "" {
		params["_source"] = *source
	}

	var reqBody any
	method := "GET"
	if *body != "" {
		var err error
		if reqBody, err = util.LoadJSONArg(*body); err != nil {
			return err
		}
		method = "POST"
	}

	status, resp, err := client.Request(method, "/"+index+"/_search", reqBody, params)
	if err != nil {
		return err
	}
	if resp, err = mustOK(status, resp); err != nil {
		return err
	}

	if *idsOnly {
		if m, ok := resp.(map[string]any); ok {
			for _, h := range hitsOf(m) {
				if hm, ok := h.(map[string]any); ok {
					fmt.Println(util.AsStr(hm["_id"]))
				}
			}
			return nil
		}
	}
	printJSON(resp)
	return nil
}

func cmdGet(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	if err := parseFlags(fs, args, nil); err != nil {
		return err
	}
	index, docID := fs.Arg(0), fs.Arg(1)
	if index == "" || docID == "" {
		return errors.New("get: index and doc_id required")
	}
	status, body, err := client.Request("GET", "/"+index+"/_doc/"+docID, nil, nil)
	if err != nil {
		return err
	}
	if body, err = mustOK(status, body); err != nil {
		return err
	}
	printJSON(body)
	return nil
}

func cmdIndex(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	body := fs.String("body", "", "JSON body (or @file)")
	refresh := fs.Bool("refresh", false, "refresh after write")
	if err := parseFlags(fs, args, map[string]bool{"refresh": true}); err != nil {
		return err
	}
	index := fs.Arg(0)
	if index == "" {
		return errors.New("index: index required")
	}
	if *body == "" {
		return errors.New("index: --body is required")
	}
	docID := fs.Arg(1)
	reqBody, err := util.LoadJSONArg(*body)
	if err != nil {
		return err
	}
	path, method := "/"+index+"/_doc", "POST"
	if docID != "" {
		path, method = "/"+index+"/_doc/"+docID, "PUT"
	}
	status, resp, err := client.Request(method, path, reqBody, refreshParam(*refresh))
	if err != nil {
		return err
	}
	if resp, err = mustOK(status, resp); err != nil {
		return err
	}
	printJSON(resp)
	return nil
}

// stringSlice collects repeatable --set flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func cmdUpdate(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	var sets stringSlice
	fs.Var(&sets, "set", "field assignment K=V (repeatable; JSON literals accepted)")
	doc := fs.String("doc", "", "JSON object (or @file) to merge")
	upsert := fs.Bool("upsert", false, "create when missing (doc_as_upsert)")
	refresh := fs.Bool("refresh", false, "refresh after write")
	if err := parseFlags(fs, args, map[string]bool{"upsert": true, "refresh": true}); err != nil {
		return err
	}
	index, docID := fs.Arg(0), fs.Arg(1)
	if index == "" || docID == "" {
		return errors.New("update: index and doc_id required")
	}
	if len(sets) == 0 && *doc == "" {
		return errors.New("update: provide --set k=v [...] or --doc <json>")
	}

	docMap := map[string]any{}
	if *doc != "" {
		loaded, err := util.LoadJSONArg(*doc)
		if err != nil {
			return err
		}
		m, ok := loaded.(map[string]any)
		if !ok {
			return errors.New("--doc must be a JSON object")
		}
		for k, v := range m {
			docMap[k] = v
		}
	}
	for _, kv := range sets {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			return fmt.Errorf("--set expects key=value, got %q", kv)
		}
		docMap[k] = util.CoerceScalar(v)
	}

	reqBody := map[string]any{"doc": docMap, "doc_as_upsert": *upsert}
	status, resp, err := client.Request("POST", "/"+index+"/_update/"+docID, reqBody, refreshParam(*refresh))
	if err != nil {
		return err
	}
	if resp, err = mustOK(status, resp); err != nil {
		return err
	}
	printJSON(resp)
	return nil
}

func cmdEdit(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	refresh := fs.Bool("refresh", false, "refresh after write")
	if err := parseFlags(fs, args, map[string]bool{"refresh": true}); err != nil {
		return err
	}
	index, docID := fs.Arg(0), fs.Arg(1)
	if index == "" || docID == "" {
		return errors.New("edit: index and doc_id required")
	}

	status, body, err := client.Request("GET", "/"+index+"/_doc/"+docID, nil, nil)
	if err != nil {
		return err
	}
	if _, err = mustOK(status, body); err != nil {
		return err
	}
	m, ok := body.(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected response: %v", body)
	}
	original, ok := m["_source"]
	if !ok {
		return fmt.Errorf("unexpected response: %v", body)
	}

	edited, err := util.EditInEditor(original)
	if err != nil {
		return err
	}
	if util.JSONEqual(edited, original) {
		fmt.Println("no changes — nothing to do")
		return nil
	}

	params := refreshParam(*refresh)
	if seq, ok1 := m["_seq_no"]; ok1 {
		if term, ok2 := m["_primary_term"]; ok2 {
			if params == nil {
				params = map[string]string{}
			}
			params["if_seq_no"] = util.AsStr(seq)
			params["if_primary_term"] = util.AsStr(term)
		}
	}

	status, resp, err := client.Request("PUT", "/"+index+"/_doc/"+docID, edited, params)
	if err != nil {
		return err
	}
	if resp, err = mustOK(status, resp); err != nil {
		return err
	}
	printJSON(resp)
	return nil
}

func cmdDelete(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	refresh := fs.Bool("refresh", false, "refresh after write")
	if err := parseFlags(fs, args, map[string]bool{"yes": true, "refresh": true}); err != nil {
		return err
	}
	index, docID := fs.Arg(0), fs.Arg(1)
	if index == "" || docID == "" {
		return errors.New("delete: index and doc_id required")
	}
	if !*yes {
		if !confirm(fmt.Sprintf("delete %s/%s? [y/N] ", index, docID)) {
			fmt.Println("aborted")
			return nil
		}
	}
	status, body, err := client.Request("DELETE", "/"+index+"/_doc/"+docID, nil, refreshParam(*refresh))
	if err != nil {
		return err
	}
	if body, err = mustOK(status, body); err != nil {
		return err
	}
	printJSON(body)
	return nil
}

func cmdDeleteByQuery(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("delete-by-query", flag.ContinueOnError)
	q := fs.String("q", "", "Lucene query string")
	body := fs.String("body", "", "JSON body (or @file)")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	refresh := fs.Bool("refresh", false, "refresh after write")
	if err := parseFlags(fs, args, map[string]bool{"yes": true, "refresh": true}); err != nil {
		return err
	}
	index := fs.Arg(0)
	if index == "" {
		return errors.New("delete-by-query: index required")
	}
	if *q == "" && *body == "" {
		return errors.New("delete-by-query: pass --q or --body")
	}
	if !*yes {
		if !confirm(fmt.Sprintf("delete-by-query on %s (q=%q)? [y/N] ", index, *q)) {
			fmt.Println("aborted")
			return nil
		}
	}
	params := map[string]string{}
	if *q != "" {
		params["q"] = *q
	}
	if *refresh {
		params["refresh"] = "true"
	}
	var reqBody any
	if *body != "" {
		var err error
		if reqBody, err = util.LoadJSONArg(*body); err != nil {
			return err
		}
	}
	status, resp, err := client.Request("POST", "/"+index+"/_delete_by_query", reqBody, params)
	if err != nil {
		return err
	}
	if resp, err = mustOK(status, resp); err != nil {
		return err
	}
	printJSON(resp)
	return nil
}

func cmdTUI(client *esclient.Client, args []string) error {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	index := fs.String("index", "", "skip the indices screen and jump into this index")
	if err := parseFlags(fs, args, nil); err != nil {
		return err
	}
	return tui.Run(client, *index)
}
