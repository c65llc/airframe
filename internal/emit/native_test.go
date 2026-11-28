// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// specConfig is the spec §6.2 modes and apple platform.
const specConfig = `version: 1
content: design-system
modes:
  appearance: { values: [light, dark], binding: runtime,  web: { light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"] } }
  density:    { values: [compact, standard, relaxed], base: standard, binding: runtime, web: { attr: data-density, also: { relaxed: "@media (pointer: coarse)" } } }
  app:        { values: [drafts, edit, review, slides, tasks, wiki], base: drafts, binding: build, web: { attr: data-app }, group: accent }
  swatch:     { values: from brands/swatches.tokens.json, binding: runtime, web: { attr: data-accent-swatch }, group: accent }
  channel:    { values: [stable, next], base: stable, binding: build, web: { attr: data-channel } }
platforms:
  apple:
    emit: [swift, colorsets]
    out: { swift: ios/Shared/DesignTokens.swift, colorsets: "ios/{app}/Assets.xcassets/Tokens/" }
    fanOut: app
`

// specTokens: accent varies on app and appearance; button-bg aliases it;
// gap varies on density; banner on channel (swift only), flag on channel
// but emitted to css alone.
const specTokens = `{
  "accent": { "$type": "number", "$value": 1, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "modes": { "app:tasks": 2, "appearance:dark": 3, "app:tasks+appearance:dark": 4 } } } },
  "button-bg": { "$type": "number", "$value": "{accent}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } },
  "gap": { "$type": "number", "$value": 8, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "modes": { "density:compact": 4 } } } },
  "pinned": { "$type": "number", "$value": 5, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "modes": { "channel:next": 6, "app:wiki": 7 } } } }
}`

func specProject(t *testing.T, toks string) *project.Project {
	t.Helper()
	cfg, ds := config.Load([]byte(specConfig))
	require.Empty(t, ds)
	set, ds := tokens.Parse(cfg, map[string][]byte{"design-system/a.tokens.json": []byte(toks)})
	require.Empty(t, ds)
	return &project.Project{Config: cfg, Tokens: set}
}

func applePlan(t *testing.T, p *project.Project) NativePlan {
	t.Helper()
	require.Equal(t, "apple", p.Config.Platforms[0].Name)
	return PlanNative(p.Config, p.Config.Platforms[0])
}

func axisNames(as []config.Axis) []string {
	out := []string{}
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func TestPlanNativeRoles(t *testing.T) {
	p := specProject(t, specTokens)
	np := applePlan(t, p)
	require.NotNil(t, np.FanOut)
	assert.Equal(t, "app", np.FanOut.Name)
	require.NotNil(t, np.Appearance)
	assert.Equal(t, "appearance", np.Appearance.Name)
	assert.Equal(t, []string{"density"}, axisNames(np.Runtime), "swatch is deferred and skipped")
	assert.Equal(t, []string{"channel"}, axisNames(np.Fixed))
	assert.Equal(t, "apple", np.Platform.Name)

	var fans []string
	for _, o := range np.Outputs() {
		fans = append(fans, o.FanValue)
	}
	assert.Equal(t, []string{"drafts", "edit", "review", "slides", "tasks", "wiki"}, fans)

	noFan := PlanNative(p.Config, config.Platform{Name: "x", Emit: []string{"swift"}})
	assert.Nil(t, noFan.FanOut)
	assert.Equal(t, []Output{{FanValue: ""}}, noFan.Outputs())
	assert.Equal(t, []string{"app", "channel"}, axisNames(noFan.Fixed))
}

func TestVariesThroughAlias(t *testing.T) {
	p := specProject(t, specTokens)
	np := applePlan(t, p)
	assert.Equal(t, []string{"app", "appearance"}, np.Varies(p.Tokens, p.Tokens.Tokens["button-bg"]))
	assert.Equal(t, []string{"density"}, np.Varies(p.Tokens, p.Tokens.Tokens["gap"]))
	assert.Equal(t, []string{"app", "channel"}, np.Varies(p.Tokens, p.Tokens.Tokens["pinned"]))
}

func TestValueAppliesFanOutAndFixed(t *testing.T) {
	p := specProject(t, specTokens)
	np := applePlan(t, p)
	val := func(name string, ctx config.Context) any {
		v, err := np.Value(p.Tokens, name, ctx)
		require.NoError(t, err)
		return v.Lit
	}
	assert.Equal(t, 4.0, val("button-bg", config.Context{"app": "tasks", "appearance": "dark"}))
	assert.Equal(t, 2.0, val("button-bg", config.Context{"app": "tasks"}), "missing appearance is its base")
	assert.Equal(t, 1.0, val("button-bg", config.Context{}), "missing fan-out is its base")
	assert.Equal(t, 5.0, val("pinned", config.Context{"channel": "next"}), "a fixed axis is always its base")
	assert.Equal(t, 7.0, val("pinned", config.Context{"app": "wiki", "channel": "next"}))
	assert.Equal(t, 4.0, val("gap", config.Context{"density": "compact"}))

	_, err := np.Value(p.Tokens, "nope", config.Context{})
	assert.Error(t, err)
}

func TestFixedWarningsOnlyWhenUsed(t *testing.T) {
	// pinned varies on channel and goes to swift: one warning.
	p := specProject(t, specTokens)
	ds := applePlan(t, p).FixedWarnings(p)
	require.Len(t, ds, 1)
	d := ds[0]
	assert.Equal(t, "emit-axis-fixed", d.Rule)
	assert.Equal(t, diag.Warn, d.Severity)
	assert.Equal(t, config.File, d.File)
	assert.Equal(t, "platforms.apple", d.Path)
	assert.Equal(t, "platform apple: channel is build-bound but not fanned out; using its base value stable", d.Message)
	assert.Equal(t, diag.DocsURL("emit-axis-fixed"), d.Docs)

	// Two tokens varying on channel still warn once.
	two := specProject(t, `{
  "a": { "$type": "number", "$value": 1, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "modes": { "channel:next": 2 } } } },
  "b": { "$type": "number", "$value": "{a}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } } }`)
	assert.Len(t, applePlan(t, two).FixedWarnings(two), 1)

	// A token varying on channel but emitted only to css is not used here.
	cssOnly := specProject(t, `{
  "a": { "$type": "number", "$value": 1, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "emit": ["css"], "modes": { "channel:next": 2 } } } },
  "gap": { "$type": "number", "$value": 8, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "modes": { "density:compact": 4 } } } } }`)
	assert.Empty(t, applePlan(t, cssOnly).FixedWarnings(cssOnly))

	// No token varies on channel: nothing.
	none := specProject(t, `{ "gap": { "$type": "number", "$value": 8, "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`)
	assert.Empty(t, applePlan(t, none).FixedWarnings(none))
}

// TestBuildReportsFixedAxis: Build passes a selected native platform's
// fixed-axis warnings through, and a filter that skips it drops them.
func TestBuildReportsFixedAxis(t *testing.T) {
	p := specProject(t, specTokens)
	_, ds := Build(p, []string{"apple"})
	var fixed []diag.Diagnostic
	for _, d := range ds {
		if d.Rule == "emit-axis-fixed" {
			fixed = append(fixed, d)
		}
	}
	require.Len(t, fixed, 1)
	assert.Contains(t, fixed[0].Message, "channel")
	_, ds = Build(p, []string{"css"})
	assert.Empty(t, ds)
}
