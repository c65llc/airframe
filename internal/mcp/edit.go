// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/report"
	"github.com/c65llc/airframe/internal/textdiff"
)

// The write pipeline every write tool ends in. A tool edits the sources in
// memory (ojson or yaml.v3 nodes, so key order and comments stay) and
// formats each touched file as airframe fmt does; commit then loads and
// builds the project from an overlay of those files, so it knows before
// writing anything whether the edit loads, builds and what lint will say.
// A preview stops there. A write lands the sources and the outputs build
// regenerates together, through the os.Root.

// checkBase refuses an edit when the caller read the record at a version
// other than now. An empty base skips the check.
func checkBase(base, now, what string) error {
	if base == "" || base == now {
		return nil
	}
	if now == "" {
		return failf("conflict: %s no longer exists; read the project again before you edit it", what)
	}
	return failf("conflict: %s changed since you read it (baseVersion %q, now %s); read it again and redo your edit on what is there now", what, base, now)
}

// A preview's format: the files whole (the default) or as unified diffs.
const (
	formatFull = "full"
	formatDiff = "diff"
)

// formatProperty is the set tools' format argument.
var formatProperty = map[string]any{
	"type": "string", "enum": []string{formatFull, formatDiff},
	"description": "With preview: \"full\" (the default) returns each source file whole; \"diff\" returns a unified diff of each against the file as it is now (a new file against /dev/null), which stays small however large the file is.",
}

// previewProperty and baseVersionProperty are the set tools' other common
// arguments.
var (
	previewProperty     = boolean("Write nothing; return the source files the edit would write, the outputs build would regenerate, and the lint report they would give. Preview first, then repeat the call without it.")
	baseVersionProperty = str("The recordVersion airframe_show returned. If the record has changed since, the edit is refused as a conflict and nothing is written. Leave it out to skip the check.")
)

// checkFormat validates format: it is "full", "diff" or absent, and only
// with preview, which it shapes.
func checkFormat(preview bool, format string) error {
	switch {
	case format != "" && format != formatFull && format != formatDiff:
		return failf("format must be %q or %q, not %q", formatFull, formatDiff, format)
	case format != "" && !preview:
		return failf("format shapes a preview: pass preview: true with it, or leave format out")
	}
	return nil
}

// previewDiffs is a diff preview's diffs: for each file the edit would
// write, the unified diff (3 lines of context) from its current content,
// or from /dev/null for a new file, to the content it would have.
func previewDiffs(current, files map[string][]byte) map[string]string {
	diffs := map[string]string{}
	for name, next := range files {
		oldName, old := "a/"+name, current[name]
		if _, exists := current[name]; !exists {
			oldName = "/dev/null"
		}
		diffs[name] = textdiff.Unified(oldName, "b/"+name, string(old), string(next), 3)
	}
	return diffs
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// lf is b with CRLF line endings made LF, as build compares outputs.
func lf(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// ruleList names the rules of ds's errors, once each, sorted: rule IDs are
// the server's own words, so a refusal may carry them unfenced.
func ruleList(ds []diag.Diagnostic) string {
	var ids []string
	for _, d := range ds {
		if d.Severity == diag.Error {
			ids = append(ids, d.Rule)
		}
	}
	slices.Sort(ids)
	return strings.Join(slices.Compact(ids), ", ")
}

// editChanged reports whether data, the edited and formatted file name,
// differs from its source old other than by formatting: a file the edit
// read but did not change is not rewritten in fmt's form.
func editChanged(name string, old, data []byte) bool {
	if bytes.Equal(old, data) {
		return false
	}
	formatted, err := format.File(name, old)
	return err != nil || !bytes.Equal(formatted, data)
}

// commit runs the pipeline for the formatted sources an edit changed.
// check, when not nil, may refuse the edited project with a message (a
// mode value that would not load where it was put); versionOf is the
// edited record's version in it.
func (s *server) commit(p *project.Project, changed map[string][]byte, preview bool, form string,
	check func(*project.Project) string, versionOf func(*project.Project) string,
) (toolResult, error) {
	sources := map[string][]byte{}
	for name, data := range changed {
		if old, ok := p.Sources[name]; ok && !editChanged(name, old, data) {
			continue
		}
		sources[name] = data
	}
	over := project.MemFS{}
	for name, data := range sources {
		over[name] = string(data)
	}
	np, ds, err := project.Load(overlay{base: s.fsys, files: over})
	if err != nil {
		return toolResult{audit: audit{preview: preview}}, err
	}
	refused := ""
	outputs := map[string][]byte{}
	switch {
	case np == nil || hasErrors(ds):
		refused = fmt.Sprintf("refused: the edit would leave the project not loading (%s); nothing was written. Preview it to see the lint report", ruleList(ds))
	case check != nil && check(np) != "":
		refused = check(np)
	default:
		gen, bds := emit.Build(np, nil)
		if hasErrors(bds) {
			refused = fmt.Sprintf("refused: the edit would make airframe build fail (%s); nothing was written. Preview it to see the lint report", ruleList(bds))
			break
		}
		for name, data := range gen {
			disk, err := fs.ReadFile(s.fsys, name)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return toolResult{audit: audit{preview: preview}}, err
			}
			if err == nil && bytes.Equal(lf(disk), data) {
				continue
			}
			outputs[name] = data
			over[name] = string(data)
		}
	}
	if refused != "" && !preview {
		return toolResult{audit: audit{preview: preview}}, toolError(refused)
	}

	// Lint the project as the write leaves it: sources and outputs.
	lp, lds := np, ds
	if len(outputs) > 0 {
		if lp, lds, err = project.Load(overlay{base: s.fsys, files: over}); err != nil {
			return toolResult{audit: audit{preview: preview}}, err
		}
	}
	res := report.Lint(lp, lds, lint.Options{})
	if res.Err != nil {
		return toolResult{audit: audit{preview: preview}}, res.Err
	}
	var lintReport bytes.Buffer
	_ = diag.WriteJSON(&lintReport, res.Diagnostics) // a bytes.Buffer never fails a write

	done := audit{preview: preview}
	if !preview {
		all := maps.Clone(sources)
		maps.Copy(all, outputs)
		// As the CLI: writing a symbolic link would replace it with a file.
		for name := range all {
			if fi, err := s.root.Lstat(filepath.FromSlash(name)); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
				return toolResult{audit: done}, failf("refused: a file the edit would write is a symbolic link; edit the file it points to by hand, or replace the link with the file. Nothing was written")
			}
		}
		written, err := project.WriteStaged(s.root, all)
		done.written = written
		if err != nil {
			return toolResult{audit: done}, err
		}
	}
	version := ""
	if refused == "" {
		version = versionOf(lp)
	}
	out := struct {
		Version       int             `json:"version"`
		Preview       bool            `json:"preview"`
		Written       []string        `json:"written"`
		Generated     []string        `json:"generated"`
		Files         any             `json:"files,omitempty"` // a full preview's sources; absent from a write
		Diffs         any             `json:"diffs,omitempty"` // a diff preview's changes; absent otherwise
		RecordVersion string          `json:"recordVersion"`
		Refused       string          `json:"refused,omitempty"` // why a previewed edit would be refused
		Lint          json.RawMessage `json:"lint"`
	}{
		Version: 1, Preview: preview,
		Written:       nonNil(slices.Sorted(maps.Keys(sources))),
		Generated:     nonNil(slices.Sorted(maps.Keys(outputs))),
		RecordVersion: version, Refused: refused, Lint: lintReport.Bytes(),
	}
	switch {
	case preview && form == formatDiff:
		out.Diffs = previewDiffs(p.Sources, sources)
	case preview:
		files := map[string]string{}
		for name, data := range sources {
			files[name] = string(data)
		}
		out.Files = files
	}
	result := fencedJSON(encodeJSON(out))
	result.audit = done
	return result, nil
}
