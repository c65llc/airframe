// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/find"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
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
	asJSON := *inv.bools["json"]
	dir, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	if f := inv.value("file"); f != "" {
		rel, err := rootRelative(dir, f)
		if err != nil {
			return inv.fail(err)
		}
		opts.File = rel
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
		if msg := filteredOut(p, query, opts); msg != "" {
			_, _ = fmt.Fprintf(inv.stderr, "airframe find: %s\n", msg)
			return ExitFindings
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

// filteredOut says why --file left no hits when the query alone matches
// tokens: how many match, and the first one's reason the file may not
// read it. It is "" when there is no --file or nothing matches without it.
func filteredOut(p *project.Project, query string, opts find.Options) string {
	if opts.File == "" || p.Tokens == nil {
		return ""
	}
	file := opts.File
	opts.File, opts.Limit = "", len(p.Tokens.Tokens)
	all := find.Rank(p, query, opts)
	if len(all) == 0 {
		return ""
	}
	_, why := lint.CanRead(p, file, p.Tokens.Tokens[all[0].Name])
	q := strconv.Quote(query)
	if len(all) == 1 {
		return fmt.Sprintf("1 token matches %s but %s may not read it (%s: %s)", q, file, all[0].Name, why)
	}
	return fmt.Sprintf("%d tokens match %s but %s may read none of them (%s: %s)", len(all), q, file, all[0].Name, why)
}

// rootRelative is file, a path given on the command line (relative to the
// working directory, or absolute), as a slash path relative to the project
// root dir. A path outside the root is an error.
func rootRelative(dir, file string) (string, error) {
	abs, err := filepath.Abs(filepath.FromSlash(strings.ReplaceAll(file, "\\", "/")))
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	rel, ok := within(root, abs)
	if !ok {
		// The two may reach the same place through different symlinks
		// (macOS's /var and /private/var): compare their real paths.
		rel, ok = within(realPath(root), realPath(abs))
	}
	if !ok {
		return "", fmt.Errorf("--file %s is outside the project root %s", file, dir)
	}
	return filepath.ToSlash(rel), nil
}

// within is p relative to root, and whether p is inside root.
func within(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// realPath is p with its symlinks resolved, as far as p exists: a file
// that does not exist keeps its own name under its resolved directory.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(realPath(parent), filepath.Base(p))
}
