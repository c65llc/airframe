// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/textdiff"
	"github.com/c65llc/airframe/internal/tokens"
)

const (
	semanticFile  = "design-system/tokens/semantic.tokens.json"
	componentFile = "design-system/tokens/component.tokens.json"
	darkFile      = "design-system/themes/dark.tokens.json"
	cssOut        = "design-system/generated/css/tokens.css"
	buttonYAML    = "design-system/components/button.yaml"
)

// writeResult is a set tool's result.
type writeResult struct {
	Version       int
	Preview       bool
	Written       []string
	Generated     []string
	Files         map[string]string
	Diffs         map[string]string
	RecordVersion string
	Refused       string
	Lint          struct {
		Version          int
		Errors, Warnings []diag.Diagnostic
	}
}

// snapshot is every file under root, to show a refused or previewed edit
// wrote nothing.
func snapshot(t *testing.T, root *os.Root) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root.FS(), name)
		out[name] = string(data)
		return err
	}))
	return out
}

// clean fails the test unless airframe fmt and airframe build would
// change nothing: what a write leaves is fmt's form, with outputs current.
func clean(t *testing.T, root *os.Root) *project.Project {
	t.Helper()
	p, ds := loaded(t, root)
	require.Empty(t, ds, "the project loads cleanly")
	assert.Empty(t, slicesOf(format.Project(p)), "fmt would change these files")
	gen, bds := emit.Build(p, nil)
	require.Empty(t, bds)
	for name, data := range gen {
		assert.Equal(t, string(data), read(t, root, name), "build would change %s", name)
	}
	return p
}

func slicesOf(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// tokenNodeJSON is the token at path in a file under root, as JSON.
func tokenNodeJSON(t *testing.T, root *os.Root, file string, path ...string) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(read(t, root, file)), &doc))
	for _, k := range path {
		next, ok := doc[k].(map[string]any)
		if !ok {
			return nil
		}
		doc = next
	}
	return doc
}

func extOf(node map[string]any) map[string]any {
	x, _ := node["$extensions"].(map[string]any)
	ns, _ := x["com.c65llc.airframe"].(map[string]any)
	return ns
}

func TestTokenSet(t *testing.T) {
	root := fixture(t, "minimal")
	var r writeResult
	ok(t, root, "airframe_token_set", map[string]any{"name": "accent-ink", "set": map[string]any{
		"$value":       "{amber.300}",
		"$description": "Text in the accent colour.",
		"emit":         []any{"css"},
		"names.css":    "--accent-ink",
	}}, &r)
	assert.Equal(t, 1, r.Version)
	assert.False(t, r.Preview)
	assert.Equal(t, []string{semanticFile}, r.Written)
	assert.Contains(t, r.Generated, cssOut, "the outputs are rebuilt")
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, r.RecordVersion)
	p := clean(t, root)
	tok := p.Tokens.Tokens["accent-ink"]
	assert.Equal(t, "amber-300", tok.Default.Alias)
	assert.Equal(t, "Text in the accent colour.", tok.Description)
	assert.Equal(t, []string{"css"}, tok.Emit)
	assert.Equal(t, map[string]string{"css": "--accent-ink"}, tok.Names)
	assert.Equal(t, tokenVersion(p, tok), r.RecordVersion)
	node := tokenNodeJSON(t, root, semanticFile, "accent-ink")
	keys := []string{}
	for k := range node {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"$type", "$value", "$description", "$extensions"}, keys)
	src := read(t, root, semanticFile)
	assert.Less(t, strings.Index(src, `"surface"`), strings.Index(src, `"accent-ink"`), "token order is kept")

	// tier, component and retired; then null removes each key.
	ok(t, root, "airframe_token_set", map[string]any{"name": "button.bg", "set": map[string]any{
		"retired": true, "tier": "component", "component": "button", "density.compact": "{accent}",
	}}, &r)
	assert.Equal(t, []string{componentFile}, r.Written)
	p, _ = loaded(t, root)
	bg := p.Tokens.Tokens["button-bg"]
	assert.True(t, bg.Retired)
	assert.Equal(t, "accent", bg.Modes["density:compact"].Alias)

	ok(t, root, "airframe_token_set", map[string]any{"name": "accent-ink", "set": map[string]any{
		"$description": nil, "emit": nil, "names.css": nil,
	}}, &r)
	ok(t, root, "airframe_token_set", map[string]any{"name": "button-bg", "set": map[string]any{
		"retired": nil, "density.compact": nil,
	}}, &r)
	p = clean(t, root)
	assert.Empty(t, p.Tokens.Tokens["accent-ink"].Description)
	assert.Nil(t, p.Tokens.Tokens["accent-ink"].Emit)
	assert.Nil(t, p.Tokens.Tokens["accent-ink"].Names)
	assert.False(t, p.Tokens.Tokens["button-bg"].Retired)
	assert.NotContains(t, extOf(tokenNodeJSON(t, root, componentFile, "button", "bg")), "density", "an emptied object goes too")
	assert.Equal(t, map[string]any{"tier": "component", "component": "button"}, extOf(tokenNodeJSON(t, root, componentFile, "button", "bg")))

	// $type on a token, then removed back to the group's.
	ok(t, root, "airframe_token_set", map[string]any{"name": "gap-sm", "set": map[string]any{"$type": "dimension"}}, &r)
	assert.Equal(t, "dimension", tokenNodeJSON(t, root, semanticFile, "gap", "sm")["$type"])
	ok(t, root, "airframe_token_set", map[string]any{"name": "gap-sm", "set": map[string]any{"$type": nil}}, &r)
	assert.NotContains(t, tokenNodeJSON(t, root, semanticFile, "gap", "sm"), "$type")
	clean(t, root)

	// Setting what is already there writes nothing.
	ok(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"$value": "{gray.50}"}}, &r)
	assert.Equal(t, []string{}, r.Written)
	assert.Equal(t, []string{}, r.Generated)

	for msg, args := range map[string]map[string]any{
		"name is required":                 {"set": map[string]any{"$value": "{gray.50}"}},
		"set is empty":                     {"name": "surface"},
		`unknown key "colour"`:             {"name": "surface", "set": map[string]any{"colour": "red"}},
		`unknown token "nope"`:             {"name": "nope", "set": map[string]any{"$value": "{gray.50}"}},
		"$value cannot be removed":         {"name": "surface", "set": map[string]any{"$value": nil}},
		"$description must be a string":    {"name": "surface", "set": map[string]any{"$description": 5}},
		"retired must be true or false":    {"name": "surface", "set": map[string]any{"retired": "yes"}},
		"emit must be an array of strings": {"name": "surface", "set": map[string]any{"emit": "css"}},
		`"density." needs a density value`: {"name": "surface", "set": map[string]any{"density.": "{gray.50}"}},
		"path and file go with create":     {"name": "surface", "path": "surface", "set": map[string]any{"$value": "{gray.50}"}},
		"not a mode key of this project":   {"name": "surface", "set": map[string]any{"modes.appearance:sepia": "{gray.50}"}},
	} {
		before := snapshot(t, root)
		got := fails(t, root, "airframe_token_set", args)
		assert.Contains(t, got, msg)
		assert.Equal(t, before, snapshot(t, root), "a refused edit writes nothing: %s", msg)
	}
}

func TestTokenSetCreate(t *testing.T) {
	root := fixture(t, "minimal")
	var r writeResult
	ok(t, root, "airframe_token_set", map[string]any{"name": "surface-raised", "create": true, "path": "surface-raised", "file": semanticFile,
		"set": map[string]any{"$type": "color", "$value": "{gray.200}", "tier": "semantic"}}, &r)
	assert.Equal(t, []string{semanticFile}, r.Written)
	assert.Contains(t, r.Generated, cssOut)
	p := clean(t, root)
	require.NotNil(t, p.Tokens.Tokens["surface-raised"])
	assert.Contains(t, read(t, root, cssOut), "--surface-raised: var(--gray-200);")

	// Into a group, and into a new file in a new directory.
	ok(t, root, "airframe_token_set", map[string]any{"name": "gap-md", "create": true, "path": "gap.md", "file": semanticFile,
		"set": map[string]any{"$value": "{space.1}", "tier": "semantic"}}, &r)
	p = clean(t, root)
	assert.Equal(t, "dimension", p.Tokens.Tokens["gap-md"].Type, "the group's $type")
	brand := "design-system/brands/sea.tokens.json"
	ok(t, root, "airframe_token_set", map[string]any{"name": "sea", "create": true, "file": brand,
		"set": map[string]any{"$type": "color", "$value": "{gray.700}", "tier": "semantic"}}, &r)
	assert.Equal(t, []string{brand}, r.Written)
	p = clean(t, root)
	assert.Equal(t, brand, p.Tokens.Tokens["sea"].File)

	for msg, args := range map[string]map[string]any{
		"already exists; leave create out":                 {"name": "surface", "create": true, "file": semanticFile, "set": map[string]any{"$value": "{gray.50}"}},
		`path "gap.xl" names token "gap-xl"`:               {"name": "gap-lg2", "create": true, "path": "gap.xl", "file": semanticFile, "set": map[string]any{"$value": "{space.1}"}},
		"file is required":                                 {"name": "fresh", "create": true, "set": map[string]any{"$type": "color", "$value": "{gray.50}"}},
		"must be a token file under the content directory": {"name": "fresh", "create": true, "file": "web/fresh.json", "set": map[string]any{"$type": "color", "$value": "{gray.50}"}},
		"a mode file":                                      {"name": "fresh", "create": true, "file": darkFile, "set": map[string]any{"$type": "color", "$value": "{gray.50}"}},
		"climbs out with ..":                               {"name": "fresh", "create": true, "file": "../x.tokens.json", "set": map[string]any{"$type": "color", "$value": "{gray.50}"}},
		"is a token":                                       {"name": "surface-x", "create": true, "path": "surface.x", "file": semanticFile, "set": map[string]any{"$type": "color", "$value": "{gray.50}"}},
		"not a valid token name":                           {"name": "Fresh", "create": true, "file": semanticFile, "set": map[string]any{"$type": "color", "$value": "{gray.50}"}},
	} {
		before := snapshot(t, root)
		assert.Contains(t, fails(t, root, "airframe_token_set", args), msg)
		assert.Equal(t, before, snapshot(t, root), msg)
	}
}

// A mode value goes where lint loads it: beside the token's other value
// for that key, into the project's mode file for the key, into a new one
// where the layout says where, or else inline in the token's own modes.
func TestTokenSetModeFile(t *testing.T) {
	root := fixture(t, "minimal")
	var r writeResult

	// The key's value already exists in the mode file: edited there.
	ok(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"modes.appearance:dark": "{gray.700}"}}, &r)
	assert.Equal(t, []string{darkFile}, r.Written)
	p := clean(t, root)
	assert.Equal(t, "gray-700", p.Tokens.Tokens["surface"].Modes["appearance:dark"].Alias)

	// No value yet, but one file holds the key: added there, in its group.
	ok(t, root, "airframe_token_set", map[string]any{"name": "gap-sm", "set": map[string]any{"modes.appearance:dark": "{space.5}"}}, &r)
	assert.Equal(t, []string{darkFile}, r.Written)
	p = clean(t, root)
	assert.Equal(t, "space-5", p.Tokens.Tokens["gap-sm"].Modes["appearance:dark"].Alias)
	assert.Equal(t, darkFile, p.Tokens.FileOf(p.Tokens.Tokens["gap-sm"].Modes["appearance:dark"]))
	assert.Equal(t, "{space.5}", tokenNodeJSON(t, root, darkFile, "gap", "sm")["$value"])

	// Removing it takes the node, and the group it leaves empty, away.
	ok(t, root, "airframe_token_set", map[string]any{"name": "gap-sm", "set": map[string]any{"modes.appearance:dark": nil}}, &r)
	assert.Nil(t, tokenNodeJSON(t, root, darkFile, "gap"))
	clean(t, root)

	// An inline value is edited inline.
	ok(t, root, "airframe_token_set", map[string]any{"name": "button-target", "set": map[string]any{"modes.density:compact": map[string]any{"value": 40, "unit": "px"}}}, &r)
	assert.Equal(t, []string{componentFile}, r.Written)
	p = clean(t, root)
	assert.Equal(t, tokens.Dim{V: 40, Unit: "px"}, p.Tokens.Tokens["button-target"].Modes["density:compact"].Lit)

	// No file holds the key, and no sibling mode file shows the layout:
	// the value goes inline, in the token's own modes.
	ok(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"modes.density:compact": "{gray.200}"}}, &r)
	assert.Equal(t, []string{semanticFile}, r.Written)
	p = clean(t, root)
	assert.Equal(t, "gray-200", p.Tokens.Tokens["surface"].Modes["density:compact"].Alias)
	assert.Equal(t, semanticFile, p.Tokens.FileOf(p.Tokens.Tokens["surface"].Modes["density:compact"]))
	ok(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"modes.density:compact": nil}}, &r)
	clean(t, root)

	// A sibling value's file shows the layout (<dir>/<value>.tokens.json):
	// the new mode file is made beside it, where lint loads it.
	cfg := read(t, root, "airframe.yaml")
	write(t, root, "airframe.yaml", strings.Replace(cfg, "values: [light, dark], binding: runtime, web: {light: \":root\", dark: [",
		"values: [light, dark, dim], binding: runtime, web: {light: \":root\", dim: \":root[data-theme='dim']\", dark: [", 1))
	_, ds := loaded(t, root)
	require.Empty(t, ds, "the dim appearance loads")
	ok(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"modes.appearance:dim": "{gray.200}"}}, &r)
	dim := "design-system/themes/dim.tokens.json"
	assert.Equal(t, []string{dim}, r.Written)
	p = clean(t, root)
	assert.Equal(t, dim, p.Tokens.FileOf(p.Tokens.Tokens["surface"].Modes["appearance:dim"]))
	assert.Equal(t, "appearance:dim", extOf(tokenNodeJSON(t, root, dim))["mode"])

	// Two files that hold the key leave no one place for a new value.
	write(t, root, "design-system/themes/dim-extra.tokens.json", `{"$extensions": {"com.c65llc.airframe": {"mode": "appearance:dim"}}, "border": {"$value": "{gray.700}"}}`+"\n")
	before := snapshot(t, root)
	msg := fails(t, root, "airframe_token_set", map[string]any{"name": "ink", "set": map[string]any{"modes.appearance:dim": "{gray.700}"}})
	assert.Contains(t, msg, "more than one token file holds mode appearance:dim")
	assert.Equal(t, before, snapshot(t, root))
}

// An edit made on a stale read is refused, and nothing is written; a
// missing baseVersion is allowed.
func TestTokenSetConflict(t *testing.T) {
	root := fixture(t, "minimal")
	var shown struct{ RecordVersion string }
	ok(t, root, "airframe_show", map[string]any{"name": "surface"}, &shown)

	// Another token's change is not a change to this one.
	ok(t, root, "airframe_token_set", map[string]any{"name": "ink", "set": map[string]any{"$description": "Body text."}}, nil)
	var again struct{ RecordVersion string }
	ok(t, root, "airframe_show", map[string]any{"name": "surface"}, &again)
	assert.Equal(t, shown, again)

	var r writeResult
	ok(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"$description": "The page."}, "baseVersion": shown.RecordVersion}, &r)
	assert.NotEqual(t, shown.RecordVersion, r.RecordVersion)
	ok(t, root, "airframe_show", map[string]any{"name": "surface"}, &again)
	assert.Equal(t, r.RecordVersion, again.RecordVersion)

	before := snapshot(t, root)
	msg := fails(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"$description": "Stale."}, "baseVersion": shown.RecordVersion})
	assert.Contains(t, msg, "conflict: token surface changed since you read it")
	msg = fails(t, root, "airframe_token_set", map[string]any{"name": "gone",
		"set": map[string]any{"$value": "{gray.50}"}, "baseVersion": shown.RecordVersion})
	assert.Contains(t, msg, "conflict: token gone no longer exists")
	assert.Equal(t, before, snapshot(t, root))

	// A mode file's change to the token is a change to it.
	ok(t, root, "airframe_show", map[string]any{"name": "surface"}, &shown)
	write(t, root, darkFile, strings.Replace(read(t, root, darkFile), `"$value": "{gray.900}"`, `"$value": "{gray.700}"`, 1))
	assert.Contains(t, fails(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"$description": "X."}, "baseVersion": shown.RecordVersion}), "conflict:")
}

// An edit that would leave the project unable to load or build is
// refused, writing nothing; its preview says why, with the lint report. A
// lint finding alone (alias-type-mismatch, say) does not stop a write, as
// with the CLI.
func TestTokenSetRefusesBreak(t *testing.T) {
	root := fixture(t, "minimal")
	before := snapshot(t, root)
	for args, rules := range map[string]map[string]any{
		"emit-failed":      {"name": "surface", "set": map[string]any{"$value": "{nowhere}"}},
		"type-unsupported": {"name": "surface", "set": map[string]any{"$type": "colour"}},
		"schema-invalid":   {"name": "surface", "set": map[string]any{"$value": map[string]any{"colorSpace": "oklch"}}},
		"mode-base-value":  {"name": "surface", "set": map[string]any{"density.standard": "{gray.200}"}},
	} {
		msg := fails(t, root, "airframe_token_set", rules)
		assert.Contains(t, msg, "refused: the edit would ", args)
		assert.Contains(t, msg, args, "the refusal names the rule")
		assert.Contains(t, msg, "nothing was written", args)
		assert.Equal(t, before, snapshot(t, root), args)

		preview := maps.Clone(rules)
		preview["preview"] = true
		var r writeResult
		ok(t, root, "airframe_token_set", preview, &r)
		assert.True(t, r.Preview)
		assert.Contains(t, r.Refused, args)
		assert.NotEmpty(t, r.Lint.Errors, "the preview's lint says why: %s", args)
		assert.Equal(t, before, snapshot(t, root), args)
	}
}

func TestTokenSetRename(t *testing.T) {
	root := fixture(t, "minimal")
	before := snapshot(t, root)
	for _, key := range []string{"name", "path"} {
		msg := fails(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{key: "page"}})
		assert.Equal(t, "a token's name is its path, so renaming it is not an edit: run `airframe rename surface <new-name>`, which also rewrites every alias and consumer read", msg, key)
	}
	assert.Equal(t, before, snapshot(t, root))
}

func TestComponentSet(t *testing.T) {
	root := fixture(t, "consumers")
	var shown struct{ RecordVersion string }
	ok(t, root, "airframe_show", map[string]any{"name": "button", "component": true}, &shown)

	var r writeResult
	ok(t, root, "airframe_component_set", map[string]any{"id": "button", "set": map[string]any{
		"noun": "Button", "ruling": "@ruling:button", "implementations.web": "web/src/button/Button.tsx",
		"owners": []any{"web/src/button/**", "apple/Sources/Button*.swift", "android/src/button/**", "windows/App/Button*.xaml"},
	}, "baseVersion": shown.RecordVersion}, &r)
	assert.Equal(t, []string{buttonYAML}, r.Written)
	p, _ := loaded(t, root)
	c := p.Components["button"]
	assert.Equal(t, "Button", c.Noun)
	assert.Equal(t, "@ruling:button", c.Ruling)
	assert.Equal(t, map[string]string{"web": "web/src/button/Button.tsx"}, c.Implementations)
	assert.Equal(t, componentVersion(p, c), r.RecordVersion)
	src := read(t, root, buttonYAML)
	assert.Contains(t, src, "tokens: [button-bg, button-ink]\n", "an untouched flow list stays flow")
	assert.Less(t, strings.Index(src, "id: button"), strings.Index(src, "noun: Button"))
	formattedYAML, err := format.YAML([]byte(src))
	require.NoError(t, err)
	assert.Equal(t, string(formattedYAML), src, "written in fmt's form")

	// null removes; shared and hostedBy.
	ok(t, root, "airframe_component_set", map[string]any{"id": "button", "set": map[string]any{"noun": nil, "implementations.web": nil, "shared": true}}, &r)
	p, _ = loaded(t, root)
	assert.Empty(t, p.Components["button"].Noun)
	assert.Empty(t, p.Components["button"].Implementations)
	assert.True(t, p.Components["button"].Shared)
	assert.NotContains(t, read(t, root, buttonYAML), "implementations", "an emptied map goes too")

	// Create.
	ok(t, root, "airframe_component_set", map[string]any{"id": "card", "create": true, "set": map[string]any{
		"noun": "Card", "tokens": []any{}, "owners": []any{"web/src/card/**"}, "hostedBy": "button",
	}}, &r)
	card := "design-system/components/card.yaml"
	assert.Equal(t, []string{card}, r.Written)
	assert.Equal(t, "id: card\nnoun: Card\ntokens: []\nowners:\n  - web/src/card/**\nhostedBy: button\n", read(t, root, card))
	p, _ = loaded(t, root)
	require.NotNil(t, p.Components["card"])

	// Conflict, then refusals.
	before := snapshot(t, root)
	msg := fails(t, root, "airframe_component_set", map[string]any{"id": "button", "set": map[string]any{"noun": "Stale"}, "baseVersion": shown.RecordVersion})
	assert.Contains(t, msg, "conflict: component button changed since you read it")
	for want, args := range map[string]map[string]any{
		"id is required":                                        {"set": map[string]any{"noun": "X"}},
		"set is empty":                                          {"id": "button"},
		"a component's ID is its file name":                     {"id": "button", "set": map[string]any{"id": "knob"}},
		`unknown component "knob"; pass create`:                 {"id": "knob", "set": map[string]any{"noun": "Knob"}},
		"already exists; leave create out":                      {"id": "card", "create": true, "set": map[string]any{"noun": "Card"}},
		`unknown key "colour"`:                                  {"id": "button", "set": map[string]any{"colour": "red"}},
		"tokens must be an array of strings":                    {"id": "button", "set": map[string]any{"tokens": "button-bg"}},
		"shared must be true or false":                          {"id": "button", "set": map[string]any{"shared": "yes"}},
		"invalid component ID":                                  {"id": "../x", "create": true, "set": map[string]any{"noun": "X"}},
		"refused: the edit would leave the project not loading": {"id": "button", "set": map[string]any{"tokens": []any{"nope"}}},
	} {
		assert.Contains(t, fails(t, root, "airframe_component_set", args), want)
		assert.Equal(t, before, snapshot(t, root), want)
	}
}

// A preview returns what the write would write and the lint it would
// give, whole or as diffs, and writes nothing.
func TestWritePreviewDiff(t *testing.T) {
	for _, tc := range []struct {
		name, tool string
		args       map[string]any
	}{
		{"update token", "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"$value": "{gray.200}"}}},
		{"new token", "airframe_token_set", map[string]any{"name": "fresh", "create": true, "file": "design-system/tokens/fresh.tokens.json", "set": map[string]any{"$type": "color", "$value": "{gray.50}", "tier": "semantic"}}},
		{"mode value", "airframe_token_set", map[string]any{"name": "ink", "set": map[string]any{"modes.appearance:dark": "{gray.200}"}}},
		{"new component", "airframe_component_set", map[string]any{"id": "button", "create": true, "set": map[string]any{"tokens": []any{"button-bg", "button-ink", "button-target", "button-gap"}, "owners": []any{"web/**"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t, "minimal")
			before := snapshot(t, root)
			full := maps.Clone(tc.args)
			full["preview"] = true
			var whole writeResult
			ok(t, root, tc.tool, full, &whole)
			assert.Equal(t, before, snapshot(t, root), "a preview writes nothing")
			assert.True(t, whole.Preview)
			require.Len(t, whole.Written, 1)
			require.Len(t, whole.Files, 1)
			assert.Nil(t, whole.Diffs)

			diffArgs := maps.Clone(full)
			diffArgs["format"] = "diff"
			var pre writeResult
			ok(t, root, tc.tool, diffArgs, &pre)
			assert.Equal(t, before, snapshot(t, root))
			assert.Nil(t, pre.Files, "a diff preview does not return whole files")
			assert.Equal(t, whole.Written, pre.Written)
			assert.Equal(t, whole.Generated, pre.Generated)
			assert.Equal(t, whole.Lint, pre.Lint)
			assert.Equal(t, whole.RecordVersion, pre.RecordVersion)
			require.Len(t, pre.Diffs, 1)

			var done writeResult
			ok(t, root, tc.tool, tc.args, &done)
			assert.False(t, done.Preview)
			assert.Nil(t, done.Files)
			assert.Nil(t, done.Diffs)
			assert.Equal(t, whole.Written, done.Written)
			assert.Equal(t, whole.Generated, done.Generated, "the preview names the outputs build would rewrite")
			assert.Equal(t, whole.Lint, done.Lint, "the preview's lint is the write's")
			assert.Equal(t, whole.RecordVersion, done.RecordVersion)
			name := done.Written[0]
			assert.Equal(t, whole.Files[name], read(t, root, name), "the preview is what was written")
			oldName, old := "a/"+name, before[name]
			if _, existed := before[name]; !existed {
				oldName = "/dev/null"
			}
			assert.Equal(t, textdiff.Unified(oldName, "b/"+name, old, read(t, root, name), 3), pre.Diffs[name], "the diff is the write's change")
		})
	}

	// format is "full" or "diff", and only shapes a preview.
	root := fixture(t, "minimal")
	args := map[string]any{"name": "surface", "set": map[string]any{"$value": "{gray.200}"}, "preview": true, "format": "patch"}
	assert.Equal(t, `format must be "full" or "diff", not "patch"`, fails(t, root, "airframe_token_set", args))
	args["format"] = "diff"
	delete(args, "preview")
	assert.Equal(t, `format shapes a preview: pass preview: true with it, or leave format out`, fails(t, root, "airframe_token_set", args))
	assert.Equal(t, `format shapes a preview: pass preview: true with it, or leave format out`,
		fails(t, root, "airframe_component_set", map[string]any{"id": "button", "create": true, "set": map[string]any{"noun": "B"}, "format": "full"}))
}

// A write rebuilds the outputs, so build --check and lint's emitted-stale
// stay quiet; the audit line names every file written.
func TestWriteRegeneratesOutputs(t *testing.T) {
	root := fixture(t, "minimal")
	s := newServer(root, Options{}, nil)
	var log strings.Builder
	s.log = &log
	in := initLine + "\n" +
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"airframe_token_set","arguments":{"name":"surface","set":{"$value":"{gray.200}"},"preview":true}}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"airframe_token_set","arguments":{"name":"surface","set":{"$value":"{gray.200}"}}}}` + "\n"
	var out strings.Builder
	require.NoError(t, s.serve(strings.NewReader(in), &out))
	lines := strings.Split(strings.TrimSuffix(log.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Regexp(t, `tool=airframe_token_set class=write preview=true outcome=ok ms=\d+ bytes=\d+$`, lines[0], "a preview writes nothing")
	assert.Regexp(t, `tool=airframe_token_set class=write preview=false outcome=ok ms=\d+ bytes=\d+ written=design-system/generated/css/tokens.css,design-system/generated/json/tokens.json,design-system/tokens/semantic.tokens.json$`, lines[1])
	assert.Contains(t, read(t, root, cssOut), "--surface: var(--gray-200);")
	clean(t, root)
	var report struct{ Errors, Warnings []diag.Diagnostic }
	ok(t, root, "airframe_lint", nil, &report)
	for _, d := range append(report.Errors, report.Warnings...) {
		assert.NotEqual(t, "emitted-stale", d.Rule)
	}
}

// Writes go through the root: a project whose files cannot be written
// fails, and says so in the audit line.
func TestWriteFailureIsReported(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes through read-only directories")
	}
	root := fixture(t, "minimal")
	dir := filepath.Join(root.Name(), "design-system", "tokens")
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if f, err := os.CreateTemp(dir, "probe"); err == nil {
		// Windows (and some filesystems) ignore a read-only directory
		// mode, so there is no failing write to provoke here.
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Skip("a read-only directory mode does not stop writes here")
	}
	msg := fails(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"$value": "{gray.200}"}})
	assert.Contains(t, msg, "could not read or write the project")
	assert.NotContains(t, read(t, root, cssOut), "--surface: var(--gray-200);", "all or nothing: no output was written either")
}

// A source that is a symbolic link is not replaced by a file.
func TestWriteRefusesSymlink(t *testing.T) {
	root := fixture(t, "minimal")
	dir := root.Name()
	real := filepath.Join(dir, "design-system", "tokens", "semantic.real")
	link := filepath.Join(dir, filepath.FromSlash(semanticFile))
	require.NoError(t, os.Rename(link, real))
	if err := os.Symlink("semantic.real", link); err != nil {
		t.Skip("no symbolic links here")
	}
	before := snapshot(t, root)
	assert.Contains(t, fails(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"$value": "{gray.200}"}}), "is a symbolic link")
	assert.Equal(t, before, snapshot(t, root))
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink)
}

// I-1: a replaced value keeps the comments on the old one, and a flow
// list stays a flow list.
func TestComponentSetKeepsComments(t *testing.T) {
	root := fixture(t, "consumers")
	write(t, root, buttonYAML, "id: button\nnoun: Button # display noun\n# The tokens the button owns.\ntokens: [button-bg, button-ink] # owned\n"+
		"owners:\n  - web/src/button/**\n  - apple/Sources/Button*.swift\n  - android/src/button/**\n  - windows/App/Button*.xaml\n")
	ok(t, root, "airframe_component_set", map[string]any{"id": "button", "set": map[string]any{
		"noun": "Primary button", "tokens": []any{"button-bg", "button-ink"},
	}}, nil)
	src := read(t, root, buttonYAML)
	assert.Contains(t, src, "noun: Primary button # display noun\n")
	assert.Contains(t, src, "# The tokens the button owns.\ntokens: [button-bg, button-ink] # owned\n")
}

// I-2: with no mode file for the key and no layout for a new one, the
// value goes inline in the token's own modes, where lint loads it.
func TestTokenSetModeInline(t *testing.T) {
	root := fixture(t, "consumers")
	var r writeResult
	ok(t, root, "airframe_token_set", map[string]any{"name": "accent", "set": map[string]any{"modes.appearance:dark": "{gray.50}"}}, &r)
	assert.Equal(t, []string{"design-system/tokens/semantic.tokens.json"}, r.Written)
	p, ds := loaded(t, root)
	require.Empty(t, ds)
	assert.Equal(t, "gray-50", p.Tokens.Tokens["accent"].Modes["appearance:dark"].Alias)
	modes, _ := extOf(tokenNodeJSON(t, root, "design-system/tokens/semantic.tokens.json", "accent"))["modes"].(map[string]any)
	assert.Equal(t, "{gray.50}", modes["appearance:dark"])
}

// m-2, m-3: only the files an edit changes are written, however their
// formatting differs from fmt's; removing a mode value that is not there
// is a no-op.
func TestWriteOnlyChangedFiles(t *testing.T) {
	root := fixture(t, "minimal")
	// The dark file, unformatted: it holds the key the removal looks in.
	write(t, root, darkFile, strings.Join(strings.Fields(read(t, root, darkFile)), " ")+"\n")
	dark := read(t, root, darkFile)
	var r writeResult
	ok(t, root, "airframe_token_set", map[string]any{"name": "gap-sm", "set": map[string]any{
		"$description": "A small gap.", "modes.appearance:dark": nil,
	}}, &r)
	assert.NotContains(t, r.Written, darkFile)
	assert.Equal(t, dark, read(t, root, darkFile), "a file the edit did not change is not reformatted")

	// An edit that changes nothing writes nothing, in an unformatted file.
	// (The fixture's generated outputs are not current, so a write still
	// regenerates them, as airframe build would.)
	root = fixture(t, "consumers")
	semantic, button := read(t, root, "design-system/tokens/semantic.tokens.json"), read(t, root, buttonYAML)
	ok(t, root, "airframe_token_set", map[string]any{"name": "accent", "set": map[string]any{"$value": "{blue.500}"}}, &r)
	assert.Empty(t, r.Written)
	assert.NotEmpty(t, r.Generated)
	ok(t, root, "airframe_component_set", map[string]any{"id": "button", "set": map[string]any{"tokens": []any{"button-bg", "button-ink"}}}, &r)
	assert.Empty(t, r.Written)
	assert.Equal(t, semantic, read(t, root, "design-system/tokens/semantic.tokens.json"))
	assert.Equal(t, button, read(t, root, buttonYAML))
}

// m-4: a modes key that is not a mode key says what one looks like.
func TestTokenSetModeKeyMessages(t *testing.T) {
	root := fixture(t, "minimal")
	msg := fails(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"modes.dark": "{gray.200}"}})
	assert.Contains(t, msg, "mode keys look like axis:value, such as appearance:dark")
	msg = fails(t, root, "airframe_token_set", map[string]any{"name": "surface", "set": map[string]any{"modes.compact": "{gray.200}"}})
	assert.Contains(t, msg, "a density value is density.<value>, such as density.compact")
}

// m-5: baseVersion is for edits, never with create.
func TestCreateRefusesBaseVersion(t *testing.T) {
	root := fixture(t, "consumers")
	before := snapshot(t, root)
	msg := fails(t, root, "airframe_token_set", map[string]any{"name": "fresh", "create": true, "file": "design-system/tokens/semantic.tokens.json",
		"set": map[string]any{"$type": "color", "$value": "{gray.50}"}, "baseVersion": "sha256:00"})
	assert.Contains(t, msg, "baseVersion is for edits")
	msg = fails(t, root, "airframe_component_set", map[string]any{"id": "card", "create": true, "set": map[string]any{"noun": "Card"}, "baseVersion": "sha256:00"})
	assert.Contains(t, msg, "baseVersion is for edits")
	assert.Equal(t, before, snapshot(t, root))
}

// m-6: a token's version covers the group $type it inherits.
func TestTokenVersionInheritsGroupType(t *testing.T) {
	root := fixture(t, "consumers")
	p, _ := loaded(t, root)
	tok := p.Tokens.Tokens["gray-50"]
	require.NotNil(t, tok)
	v := tokenVersion(p, tok)
	file := tok.File
	p.Sources[file] = []byte(strings.Replace(string(p.Sources[file]), `"$type": "color"`, `"$type": "dimension"`, 1))
	assert.NotEqual(t, v, tokenVersion(p, tok), "the inherited $type is part of the record")
}

// m-7: a created component's tokens are a flow list.
func TestComponentCreateTokensFlow(t *testing.T) {
	root := fixture(t, "consumers")
	var r writeResult
	ok(t, root, "airframe_component_set", map[string]any{"id": "card", "create": true, "preview": true, "set": map[string]any{
		"noun": "Card", "tokens": []any{"button-bg", "button-ink"}, "owners": []any{"web/src/card/**"},
	}}, &r)
	assert.Contains(t, r.Files["design-system/components/card.yaml"], "tokens: [button-bg, button-ink]\n")
}
