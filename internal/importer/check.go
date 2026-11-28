// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package importer

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/cssvalue"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
)

// CheckResult is what an equivalence check compared: Environments browser
// environments, Properties custom properties (the union of both sides),
// and Comparisons = Environments × Properties.
type CheckResult struct {
	Environments, Properties, Comparisons int
	Diagnostics                           []diag.Diagnostic
}

// maxNotEquivalent caps the per-property import-not-equivalent reports;
// the rest are counted in one summary.
const maxNotEquivalent = 20

// emitCSS writes a platform's CSS; a variable so a test can hand-edit the
// generated file.
var emitCSS = emit.CSS

// Check proves that the CSS Airframe generates from files (the imported
// token files) resolves every custom property to the value old resolves
// it to, in every environment either side can tell apart. The project is
// airframe.yaml (configYAML) plus files only, emitted with the first
// platform in config order that emits css. A property declared on one
// side only is a mismatch.
func Check(cfg *config.Config, configYAML []byte, files map[string][]byte, old []*css.Sheet) CheckResult {
	var res CheckResult
	pl, ok := cssPlatform(cfg)
	if !ok {
		res.Diagnostics = []diag.Diagnostic{errorDiag(RuleNothing, config.File, 0,
			"no platform emits css; add one to airframe.yaml so the import can be checked")}
		return res
	}
	gen, ds := generate(configYAML, files, pl)
	if ds != nil {
		res.Diagnostics = ds
		return res
	}
	envs, err := css.Envs(slices.Concat(old, gen)...)
	if err != nil {
		res.Diagnostics = []diag.Diagnostic{envsDiag(err)}
		return res
	}
	oldC, err := css.Compile(old)
	if err == nil {
		var genC *css.Compiled
		genC, err = css.Compile(gen)
		if err == nil {
			res.Environments = len(envs)
			res.Diagnostics = compare(&res, envs, old, gen, oldC, genC)
			return res
		}
	}
	res.Diagnostics = []diag.Diagnostic{errorDiag(RuleNothing, config.File, 0,
		fmt.Sprintf("internal: could not compile the stylesheets to compare: %v", err))}
	return res
}

// cssPlatform is the first platform in config order that emits css.
func cssPlatform(cfg *config.Config) (config.Platform, bool) {
	for _, pl := range cfg.Platforms {
		if slices.Contains(pl.Emit, "css") {
			return pl, true
		}
	}
	return config.Platform{}, false
}

// generate loads the in-memory project and parses its emitted CSS. Any
// diagnostic means the check cannot run; they are returned non-nil.
func generate(configYAML []byte, files map[string][]byte, pl config.Platform) ([]*css.Sheet, []diag.Diagnostic) {
	fsys := project.MemFS{config.File: string(configYAML)}
	for p, b := range files {
		fsys[p] = string(b)
	}
	p, ds, err := project.Load(fsys)
	if err != nil {
		return nil, []diag.Diagnostic{errorDiag(RuleNothing, config.File, 0,
			fmt.Sprintf("could not load the imported project: %v", err))}
	}
	if len(ds) > 0 || p == nil {
		if len(ds) == 0 {
			ds = []diag.Diagnostic{errorDiag(RuleNothing, config.File, 0, "the imported project did not load")}
		}
		return nil, ds
	}
	out, err := emitCSS(p, pl)
	if err != nil {
		return nil, []diag.Diagnostic{errorDiag("emit-failed", config.File, 0,
			fmt.Sprintf("platform %s: css: %v", pl.Name, err))}
	}
	var sheets []*css.Sheet
	var parsed []diag.Diagnostic
	for _, f := range slices.Sorted(maps.Keys(out)) {
		sh, pds := css.Parse(f, out[f])
		sheets = append(sheets, sh)
		parsed = append(parsed, pds...)
	}
	if len(parsed) > 0 {
		return nil, parsed
	}
	return sheets, nil
}

// envsDiag turns a css.Envs error into a diagnostic under the ID it
// carries ("[file:line: ]id: message").
func envsDiag(err error) diag.Diagnostic {
	msg := err.Error()
	for _, id := range IDs() {
		at := strings.Index(msg, id+": ")
		if at < 0 || (at > 0 && !strings.HasSuffix(msg[:at], ": ")) {
			continue
		}
		file, line := config.File, 0
		if loc := strings.TrimSuffix(msg[:at], ": "); loc != "" {
			file = loc
			if i := strings.LastIndexByte(loc, ':'); i >= 0 {
				if n, err := strconv.Atoi(loc[i+1:]); err == nil {
					file, line = loc[:i], n
				}
			}
		}
		return errorDiag(id, file, line, msg[at+len(id)+2:])
	}
	return errorDiag(RuleNothing, config.File, 0, "could not enumerate environments: "+msg)
}

// decl locates a property's first declaration.
type decl struct {
	file string
	line int
}

// compare resolves both sides in every environment and reports each
// property that differs anywhere.
func compare(res *CheckResult, envs []css.Env, old, gen []*css.Sheet, oldC, genC *css.Compiled) []diag.Diagnostic {
	first := map[string]decl{}
	names := map[string]bool{}
	for _, sh := range old {
		for _, r := range sh.Rules {
			for _, d := range r.Decls {
				names[d.Name] = true
				if _, ok := first[d.Name]; !ok {
					first[d.Name] = decl{sh.File, d.Line}
				}
			}
		}
	}
	for _, sh := range gen {
		for _, r := range sh.Rules {
			for _, d := range r.Decls {
				names[d.Name] = true
				if _, ok := first[d.Name]; !ok {
					first[d.Name] = decl{config.File, 0}
				}
			}
		}
	}
	props := slices.Sorted(maps.Keys(names))
	res.Properties = len(props)
	res.Comparisons = len(envs) * len(props)
	if res.Comparisons == 0 {
		return []diag.Diagnostic{errorDiag(RuleNothing, config.File, 0,
			"nothing to compare: neither the original nor the generated CSS declares a custom property")}
	}
	type mismatch struct {
		env      css.Env
		old, gen string
		more     int
	}
	found := map[string]*mismatch{}
	for _, e := range envs {
		o, g := oldC.Resolve(e), genC.Resolve(e)
		for _, name := range props {
			ov, oin := o[name]
			gv, gin := g[name]
			if oin == gin && (!oin || cssvalue.Equal(name, ov, gv)) {
				continue
			}
			if m := found[name]; m != nil {
				m.more++
				continue
			}
			found[name] = &mismatch{env: e, old: show(ov, oin), gen: show(gv, gin)}
		}
	}
	files := map[string]int{}
	for i, sh := range old {
		if _, ok := files[sh.File]; !ok {
			files[sh.File] = i
		}
	}
	files[config.File] = len(old)
	bad := slices.SortedFunc(maps.Keys(found), func(a, b string) int {
		da, db := first[a], first[b]
		if c := files[da.file] - files[db.file]; c != 0 {
			return c
		}
		if c := da.line - db.line; c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	var out []diag.Diagnostic
	for i, name := range bad {
		if i == maxNotEquivalent {
			file := config.File
			if len(old) > 0 {
				file = old[0].File
			}
			out = append(out, errorDiag(RuleNotEquivalent, file, 0,
				fmt.Sprintf("… and %d more properties differ", len(bad)-maxNotEquivalent)))
			break
		}
		m := found[name]
		msg := fmt.Sprintf("%s differs with %s: the original CSS gives %s, the generated CSS gives %s",
			name, renderEnv(m.env), m.old, m.gen)
		if m.more > 0 {
			msg += fmt.Sprintf(" (+%d more environments)", m.more)
		}
		d := first[name]
		out = append(out, errorDiag(RuleNotEquivalent, d.file, d.line, msg))
	}
	return out
}

// show renders one side's value for a message.
func show(c css.Computed, declared bool) string {
	switch {
	case !declared:
		return "(not declared)"
	case c.Invalid:
		return "(invalid)"
	}
	return c.Value
}

// renderEnv renders e as "attr=value, …; media=value, …", each part sorted
// by name, unset entries omitted and "*" (a value no selector names) as
// <other>.
func renderEnv(e css.Env) string {
	part := func(m map[string]string) string {
		var kv []string
		for _, k := range slices.Sorted(maps.Keys(m)) {
			v := m[k]
			if v == "*" {
				v = "<other>"
			}
			kv = append(kv, k+"="+v)
		}
		return strings.Join(kv, ", ")
	}
	var parts []string
	for _, p := range []string{part(e.Attrs), part(e.Media)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "no attributes or media features set"
	}
	return strings.Join(parts, "; ")
}

func errorDiag(rule, file string, line int, msg string) diag.Diagnostic {
	return diag.Diagnostic{Rule: rule, Severity: diag.Error, File: file, Line: line, Message: msg, Docs: diag.DocsURL(rule)}
}
