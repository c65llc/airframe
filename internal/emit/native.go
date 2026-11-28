// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"fmt"
	"slices"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// nativeFormats are the emit formats of the native shells. They resolve a
// build-bound axis they do not fan out over at its base value, where css
// and json carry every mode.
var nativeFormats = []string{"colorsets", "kotlin", "swift", "xaml"}

// NativePlan splits the loaded axes of a native platform by the role each
// plays in its outputs: FanOut is the build-bound axis with one output set
// per value; Appearance is the axis named appearance; Runtime are the other
// runtime-bound axes, parameters of the generated code; Fixed are the other
// build-bound axes, resolved at their base value. Deferred axes, whose
// values a later stage loads, play no role. Runtime and Fixed keep config
// order.
type NativePlan struct {
	Platform   config.Platform
	FanOut     *config.Axis
	Appearance *config.Axis
	Runtime    []config.Axis
	Fixed      []config.Axis
}

// Output is one output set of a native platform: FanValue is the fan-out
// axis's value, "" when the platform does not fan out.
type Output struct{ FanValue string }

// PlanNative assigns each of cfg's loaded axes its role on pl.
func PlanNative(cfg *config.Config, pl config.Platform) NativePlan {
	np := NativePlan{Platform: pl}
	for _, a := range cfg.Axes {
		if a.Deferred() {
			continue
		}
		switch {
		case pl.FanOut != "" && a.Name == pl.FanOut:
			np.FanOut = &a
		case a.Name == "appearance":
			np.Appearance = &a
		case a.Binding == "build":
			np.Fixed = append(np.Fixed, a)
		default:
			np.Runtime = append(np.Runtime, a)
		}
	}
	return np
}

// Outputs lists the platform's output sets: one per fan-out value, in
// config order, or a single one when it does not fan out.
func (np NativePlan) Outputs() []Output {
	if np.FanOut == nil {
		return []Output{{}}
	}
	out := make([]Output, len(np.FanOut.Values))
	for i, v := range np.FanOut.Values {
		out[i] = Output{FanValue: v}
	}
	return out
}

// Varies lists, sorted, the axes t's value can vary on: those named by the
// mode keys of every token t reaches through aliases.
func (np NativePlan) Varies(s *tokens.Set, t *tokens.Token) []string {
	var out []string
	for _, k := range modeKeys(s, t) {
		for _, pr := range pairsOf(k) {
			out = append(out, pr[0])
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Value resolves the named token in ctx as the platform renders it: the
// fan-out, appearance and runtime axes take ctx's value (the fan-out's is
// the output's), or their base when ctx leaves them unset, and every fixed
// axis takes its base whatever ctx says.
func (np NativePlan) Value(s *tokens.Set, name string, ctx config.Context) (*tokens.Value, error) {
	full := config.Context{}
	given := func(a config.Axis) {
		if v, ok := ctx[a.Name]; ok {
			full[a.Name] = v
		} else {
			full[a.Name] = base(a)
		}
	}
	if np.FanOut != nil {
		given(*np.FanOut)
	}
	if np.Appearance != nil {
		given(*np.Appearance)
	}
	for _, a := range np.Runtime {
		given(a)
	}
	for _, a := range np.Fixed {
		full[a.Name] = base(a)
	}
	return s.Resolve(name, full)
}

// FixedWarnings reports, once per fixed axis, that the platform resolves it
// at its base value (emit-axis-fixed, a warning), but only when some token
// emitted to one of the platform's native formats varies on it: an axis no
// output depends on loses nothing.
func (np NativePlan) FixedWarnings(p *project.Project) []diag.Diagnostic {
	used := map[string]bool{}
	for _, format := range np.Platform.Emit {
		if !slices.Contains(nativeFormats, format) {
			continue
		}
		for _, t := range emitted(p, format) {
			for _, a := range np.Varies(p.Tokens, t) {
				used[a] = true
			}
		}
	}
	var out []diag.Diagnostic
	for _, a := range np.Fixed {
		if !used[a.Name] {
			continue
		}
		out = append(out, diag.Diagnostic{
			Rule: "emit-axis-fixed", Severity: diag.Warn, File: config.File,
			Path: "platforms." + np.Platform.Name,
			Message: fmt.Sprintf("platform %s: %s is build-bound but not fanned out; using its base value %s",
				np.Platform.Name, a.Name, base(a)),
			Docs: diag.DocsURL("emit-axis-fixed"),
		})
	}
	return out
}

// base is a's base value, or its first value for an axis built by hand
// without one (config.Load never returns such an axis).
func base(a config.Axis) string {
	if a.Base != "" || len(a.Values) == 0 {
		return a.Base
	}
	return a.Values[0]
}
