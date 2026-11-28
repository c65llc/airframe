// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package report

import (
	"slices"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
)

// Lint is lint's result for a project as project.Load left it: the rules'
// findings, or for a project that did not load (p nil) its load
// diagnostics alone. res.Err is set when an I/O failure left the findings
// incomplete.
func Lint(p *project.Project, ds []diag.Diagnostic, opts lint.Options) lint.Result {
	if p == nil {
		return LoadOnly(ds)
	}
	return lint.Run(p, ds, opts)
}

// LoadOnly is the lint result of a project that did not load: its load
// diagnostics, every one an error, and no tallies since no rule ran.
func LoadOnly(ds []diag.Diagnostic) lint.Result {
	res := lint.Result{Diagnostics: slices.Clone(ds), Exemptions: []lint.Exemption{}}
	for i := range res.Diagnostics {
		res.Diagnostics[i].Docs = diag.DocsURL(res.Diagnostics[i].Rule)
		res.Errors++
	}
	diag.Sort(res.Diagnostics)
	return res
}
