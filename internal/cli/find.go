// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/find"
)

// runFind ranks the project's tokens against a free-text query.
func runFind(inv *invocation) int {
	if len(inv.args) == 0 {
		return inv.usageError("find takes a query")
	}
	query := strings.Join(inv.args, " ")
	if len(find.Words(query)) == 0 {
		return inv.usageError("the query %q has no words to search for", query)
	}
	opts := find.Options{Type: inv.value("type"), Retired: *inv.bools["retired"]}
	if v := inv.value("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return inv.usageError("--limit takes a positive number, not %q", v)
		}
		opts.Limit = n
	}
	if f := inv.value("file"); f != "" {
		opts.File = path.Clean(strings.ReplaceAll(f, "\\", "/"))
		opts.File = strings.TrimPrefix(opts.File, "./")
	}
	asJSON := *inv.bools["json"]
	_, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	if p == nil {
		diag.WriteText(inv.stderr, ds, nil)
		return ExitFindings
	}
	hits := find.Rank(p, query, opts)
	if len(hits) == 0 {
		if asJSON {
			_, _ = fmt.Fprintln(inv.stdout, "[]")
		}
		msg := "no token matches " + strconv.Quote(query)
		if p.Tokens != nil {
			if s := closest(strings.Join(find.Words(query), "-"), slices.Collect(maps.Keys(p.Tokens.Tokens))); s != "" {
				msg += "; did you mean " + s + "?"
			}
		}
		_, _ = fmt.Fprintf(inv.stderr, "airframe find: %s\n", msg)
		return ExitFindings
	}
	if asJSON {
		enc := json.NewEncoder(inv.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(hits) // a write error is Main's to report
		return ExitOK
	}
	var b strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&b, "%s  %s  %s  %s", h.Name, orNone(h.Tier), orNone(h.Type), orNone(h.Value))
		if h.Description != "" {
			b.WriteString("  — " + h.Description)
		}
		b.WriteString("\n")
		if h.Why != "" {
			b.WriteString("  " + h.Why + "\n")
		}
	}
	_, _ = inv.stdout.Write([]byte(b.String()))
	return ExitOK
}
