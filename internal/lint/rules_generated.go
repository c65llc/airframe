// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"bytes"
	"io/fs"
	"maps"
	"slices"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
)

// ruleEmittedStale compares every generated output with the file on disk:
// a missing file or different bytes is stale. It uses the outputs the
// caller generated (Options.Generated) so lint and build cannot disagree,
// and builds them itself only when none were given. CRLF line endings on
// disk do not count as a difference. A build failure is reported, never
// read as clean.
func ruleEmittedStale(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "emitted-stale"
	tally := diag.Tally{Noun: "files"}
	if len(p.Config.Platforms) == 0 {
		tally.NotApplicable = "no platforms configured"
		return nil, tally
	}
	gen := c.Generated
	var out []diag.Diagnostic
	if gen == nil {
		gen, out = emit.Build(p, nil)
	}
	if len(gen) == 0 && len(out) == 0 {
		tally.NotApplicable = "no configured platform emits a format airframe builds"
		return nil, tally
	}
	for _, path := range slices.Sorted(maps.Keys(gen)) {
		tally.Files++
		tally.Items++
		// A file that cannot be read, missing or not, is stale: the build rewrites it.
		disk, err := fs.ReadFile(p.FS, path)
		if err == nil && bytes.Equal(bytes.ReplaceAll(disk, []byte("\r\n"), []byte("\n")), gen[path]) {
			continue
		}
		out = append(out, diag.Diagnostic{
			Rule: id, Severity: diag.Error, File: path,
			Message: path + " is stale — run `airframe build`", Docs: diag.DocsURL(id),
		})
	}
	return out, tally
}
