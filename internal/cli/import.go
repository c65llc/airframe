// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/importer"
)

// importTargets are the token files an import writes, under <content>/.
// Either one existing refuses the import unless --force.
var importTargets = []string{"tokens/primitive.tokens.json", "tokens/semantic.tokens.json"}

// runImport is "import css <file>...": it maps the stylesheets onto the
// config's mode axes, proves the CSS Airframe would generate from the
// result equivalent to them, and writes the token files. The stylesheets
// are paths relative to the working directory, inside the project root.
func runImport(inv *invocation) int {
	if len(inv.args) == 0 || inv.args[0] != "css" {
		return inv.usageError("import takes css and one or more stylesheets")
	}
	names := inv.args[1:]
	if len(names) == 0 {
		return inv.usageError("import css takes one or more stylesheets")
	}
	dir, err := inv.rootDir()
	if err != nil {
		return inv.fail(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()

	cfgData, err := root.ReadFile(config.File)
	if errors.Is(err, fs.ErrNotExist) {
		writeDiags(inv.stdout, []diag.Diagnostic{{
			Rule: "config-missing", Severity: diag.Error, File: config.File,
			Message: "no airframe.yaml at the project root; import runs inside a project", Docs: diag.DocsURL("config-missing"),
		}})
		return ExitFindings
	}
	if err != nil {
		return inv.fail(err)
	}
	cfg, ds := config.Load(cfgData)
	if cfg == nil {
		diag.WriteText(inv.stdout, ds, nil)
		return ExitFindings
	}

	// Parse each stylesheet once: the sheets feed both the import and the
	// equivalence check.
	var sheets []*css.Sheet
	var importDiags []diag.Diagnostic
	for _, name := range names {
		rel, err := projectPath(dir, name)
		if err != nil {
			return inv.fail(err)
		}
		data, err := root.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			return inv.fail(fmt.Errorf("read %s: %w", name, err))
		}
		sh, pds := css.Parse(rel, data)
		sheets = append(sheets, sh)
		importDiags = append(importDiags, pds...)
	}
	res := importer.Import(cfg, sheets)
	importDiags = append(importDiags, res.Diagnostics...)
	diag.Sort(importDiags)
	writeDiags(inv.stdout, importDiags)

	summary := fmt.Sprintf("imported %d tokens from %d declarations (%d non-custom ignored)",
		res.Tokens, res.Declarations, res.Ignored)
	failed := hasErrors(importDiags)
	if failed {
		summary += "; equivalence not checked (import errors)"
	} else {
		// Check's diagnostics are in its own order: a capped list ends
		// with its "… and N more" line.
		chk := importer.Check(cfg, cfgData, res.Files, sheets)
		writeDiags(inv.stdout, chk.Diagnostics)
		failed = hasErrors(chk.Diagnostics)
		switch {
		case !failed:
			summary += fmt.Sprintf("; equivalent in %d environments across %d properties", chk.Environments, chk.Properties)
		case chk.Comparisons > 0:
			summary += fmt.Sprintf("; not equivalent: compared %d environments across %d properties", chk.Environments, chk.Properties)
		default:
			summary += "; equivalence not checked (check errors)"
		}
	}
	_, _ = fmt.Fprintln(inv.stdout, summary)
	if failed {
		return ExitFindings
	}

	outs := slices.Sorted(maps.Keys(res.Files))
	if *inv.bools["dry-run"] {
		for _, name := range outs {
			_, _ = fmt.Fprintln(inv.stdout, "would write", name)
		}
		return ExitOK
	}
	if !*inv.bools["force"] {
		var existing []string
		for _, t := range importTargets {
			name := path.Join(cfg.Content, t)
			_, err := root.Lstat(filepath.FromSlash(name))
			switch {
			case err == nil:
				existing = append(existing, name)
			case !errors.Is(err, fs.ErrNotExist):
				return inv.fail(err)
			}
		}
		if len(existing) > 0 {
			_, _ = fmt.Fprintf(inv.stderr, "airframe import: already exist; use --force to overwrite: %s\n",
				strings.Join(existing, ", "))
			return ExitUsage
		}
	}
	for _, name := range outs {
		if err := writeFile(root, name, res.Files[name]); err != nil {
			return inv.fail(err)
		}
		_, _ = fmt.Fprintln(inv.stdout, "wrote", name)
	}
	// Under --force, a target this run did not produce is left over from
	// an earlier import: remove it, so the project on disk is what the
	// equivalence check proved.
	for _, name := range staleTargets(cfg.Content, res.Files) {
		err := root.Remove(filepath.FromSlash(name))
		switch {
		case err == nil:
			_, _ = fmt.Fprintln(inv.stdout, "removed", name)
		case !errors.Is(err, fs.ErrNotExist):
			return inv.fail(err)
		}
	}
	return ExitOK
}

// staleTargets lists, in order, the import targets under content that
// files does not hold.
func staleTargets(content string, files map[string][]byte) []string {
	var out []string
	for _, t := range importTargets {
		name := path.Join(content, t)
		if _, ok := files[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}

// projectPath is name, a path relative to the working directory or
// absolute, as a slash path relative to the project root dir. A path
// outside the project is an error: the import reads only through the root.
func projectPath(dir, name string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the project root %s", name, dir)
	}
	return filepath.ToSlash(rel), nil
}

// writeDiags writes ds as text in the order given, one per line.
func writeDiags(w io.Writer, ds []diag.Diagnostic) {
	for _, d := range ds {
		diag.WriteText(w, []diag.Diagnostic{d}, nil)
	}
}
