// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"bytes"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/project"
)

// ruleNotFormatted flags a source file whose bytes differ from the
// formatter's output. CRLF line endings alone do not count. A file that
// does not parse is skipped (its parse diagnostic already reports it) and
// is not counted as checked.
func ruleNotFormatted(p *project.Project, _ *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "not-formatted"
	tally := diag.Tally{Noun: "files"}
	var out []diag.Diagnostic
	for _, f := range format.Paths(p) {
		src := p.Sources[f]
		got, err := format.File(f, src)
		if err != nil {
			continue
		}
		tally.Files++
		tally.Items++
		if bytes.Equal(got, bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))) {
			continue
		}
		out = append(out, diag.Diagnostic{
			Rule: id, Severity: diag.Error, File: f,
			Message: f + " is not formatted — run `airframe fmt`", Docs: diag.DocsURL(id),
		})
	}
	return out, tally
}
