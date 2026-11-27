// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package selftest proves the linter still catches what it claims to. It
// lints embedded fixture projects: fixtures/flag/<id> exists to trip rule
// <id>, one project per rule ID (IDs that stop loading need a project of
// their own), and fixtures/clean must produce no finding at all. A gate
// whose self-test fails is not believed, however clean its real run reads.
package selftest

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
)

//go:embed fixtures
var fixtures embed.FS

const (
	flagDir  = "fixtures/flag"
	cleanDir = "fixtures/clean"
)

// Result is a self-test run. Flagged counts the rule IDs that fired on
// their flag fixture; Ignored counts the IDs that stayed silent on the
// clean fixture. OK is true when every ID did both. Failures says, one line
// each, what went wrong, naming the rule ID.
type Result struct {
	Flagged, Ignored int
	OK               bool
	Failures         []string
}

// Run self-tests every rule ID in lint.AllIDs against the embedded fixtures.
func Run() Result { return run(fixtures, lint.AllIDs()) }

func run(fsys fs.FS, ids []string) Result {
	var r Result
	fail := func(format string, a ...any) { r.Failures = append(r.Failures, fmt.Sprintf(format, a...)) }

	for _, id := range ids {
		dir := path.Join(flagDir, id)
		if _, err := fs.Stat(fsys, dir); err != nil {
			fail("flag: no fixture for %s", id)
			continue
		}
		ds, err := lintDir(fsys, dir)
		if err != nil {
			fail("flag: %s: %v", dir, err)
			continue
		}
		if slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Rule == id }) {
			r.Flagged++
		} else {
			fail("flag: %s did not fire on %s", id, dir)
		}
	}
	ents, err := fs.ReadDir(fsys, flagDir)
	if err != nil {
		fail("flag: %v", err)
	}
	for _, e := range ents {
		if !slices.Contains(ids, e.Name()) {
			fail("flag: %s names no rule ID", path.Join(flagDir, e.Name()))
		}
	}

	ds, err := lintDir(fsys, cleanDir)
	if err != nil {
		fail("clean: %s: %v", cleanDir, err)
	} else {
		fired := map[string]bool{}
		for _, d := range ds {
			if fired[d.Rule] {
				continue
			}
			fired[d.Rule] = true
			at := d.File
			if d.Line > 0 {
				at = fmt.Sprintf("%s:%d", d.File, d.Line)
			}
			fail("clean: %s fired: %s: %s", d.Rule, at, d.Message)
		}
		for _, id := range ids {
			if !fired[id] {
				r.Ignored++
			}
		}
	}
	r.OK = len(r.Failures) == 0
	return r
}

// lintDir loads the project rooted at dir and lints it as `airframe lint`
// does, returning every finding: the load diagnostics alone when loading
// stopped, else the load diagnostics and every rule's.
func lintDir(fsys fs.FS, dir string) ([]diag.Diagnostic, error) {
	root, err := fs.Sub(fsys, dir)
	if err != nil {
		return nil, err
	}
	p, ds, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return ds, nil
	}
	return lint.Run(p, ds, lint.Options{}).Diagnostics, nil
}
