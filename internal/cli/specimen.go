// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/specimen"
)

// specimenCommand is specimen's entry in Commands.
var specimenCommand = Command{
	Name:    "specimen",
	Summary: "Write colour, type and spacing sheets as SVG (one set per fan-out value) to <content>/generated/specimen/; build does not write them.",
	Flags:   []Flag{{Name: "out", Arg: "D", Summary: "write the sheets to D, a directory inside the project, instead"}},
}

// runSpecimen renders every sheet first, then writes them all, so a failure
// to render leaves the directory untouched. The files are overwritten.
func runSpecimen(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	dir, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	if p == nil || hasErrors(ds) {
		diag.WriteText(inv.stderr, ds, nil)
		return ExitFindings
	}
	out := path.Join(p.Config.Content, "generated", "specimen")
	if v := inv.value("out"); v != "" {
		if len(v) >= 2 && v[1] == ':' && (v[0] >= 'A' && v[0] <= 'Z' || v[0] >= 'a' && v[0] <= 'z') {
			return inv.usageError("--out %s has a drive letter; give a directory inside the project", v)
		}
		out = path.Clean(strings.ReplaceAll(v, "\\", "/"))
	}
	sheets, err := specimen.Render(p)
	if err != nil {
		return inv.fail(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	for _, name := range slices.Sorted(maps.Keys(sheets)) {
		rel := path.Join(out, name)
		if err := writeFile(root, rel, sheets[name]); err != nil {
			return inv.fail(err)
		}
		_, _ = fmt.Fprintln(inv.stdout, "wrote", rel)
	}
	return ExitOK
}

// Registered here, not in the handlers literal, to keep that table's diff
// small while sibling commands land.
func init() { handlers["specimen"] = runSpecimen }
