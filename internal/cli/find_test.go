// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/schema"
)

const findConfig = `version: 1
content: design-system
platforms:
  web:
    emit: [css]
    out: {css: web/generated/tokens.css}
    consumers: ["web/**/*.tsx"]
`

// findTok is one token of the find fixture.
type findTok struct {
	path        string // dotted DTCG path
	typ, tier   string
	component   string
	description string
	value       any // a literal, or an {alias} string
	retired     bool
}

func lit(v string) any { return v }

var (
	gray    = map[string]any{"colorSpace": "oklch", "components": []float64{0.6, 0.01, 250}}
	red     = map[string]any{"colorSpace": "oklch", "components": []float64{0.55, 0.2, 25}}
	blue    = map[string]any{"colorSpace": "oklch", "components": []float64{0.5, 0.2, 260}}
	space2  = map[string]any{"value": 8, "unit": "px"}
	space4  = map[string]any{"value": 16, "unit": "px"}
	radius2 = map[string]any{"value": 4, "unit": "px"}
)

var findTokens = []findTok{
	{"gray.500", "color", "primitive", "", "A muted mid gray.", gray, false},
	{"red.600", "color", "primitive", "", "", red, false},
	{"blue.600", "color", "primitive", "", "", blue, false},
	{"space.2", "dimension", "primitive", "", "", space2, false},
	{"space.4", "dimension", "primitive", "", "", space4, false},
	{"radius.2", "dimension", "primitive", "", "", radius2, false},
	{"text.muted", "color", "semantic", "", "Secondary text on surfaces.", "{gray.500}", false},
	{"text.primary", "color", "semantic", "", "Default body text.", "{gray.500}", false},
	{"text.legacy-muted", "color", "semantic", "", "Old muted text; use text-muted.", "{gray.500}", true},
	{"surface.card", "color", "semantic", "", "Background of cards.", "{gray.500}", false},
	{"surface.page", "color", "semantic", "", "Background of the page.", "{gray.500}", false},
	{"accent.ink", "color", "semantic", "", "Text on accent fills.", "{blue.600}", false},
	{"accent.bg", "color", "semantic", "", "Accent fill.", "{blue.600}", false},
	{"danger.bg", "color", "semantic", "", "Fill of destructive actions.", "{red.600}", false},
	{"danger.ink", "color", "semantic", "", "Text on danger fills.", "{red.600}", false},
	{"gap", "dimension", "semantic", "", "Standard spacing between related items.", "{space.2}", false},
	{"gap.large", "dimension", "semantic", "", "Spacing between sections.", "{space.4}", false},
	{"corner", "dimension", "semantic", "", "Rounding of containers.", "{radius.2}", false},
	{"button.bg", "color", "component", "button", "", "{accent.bg}", false},
	{"button.gap", "dimension", "component", "button", "", "{gap}", false},
	{"card.bg", "color", "component", "card", "", "{surface.card}", false},
	{"card.gap", "dimension", "component", "card", "", "{gap.large}", false},
}

// findProject writes the find fixture project and returns its root.
func findProject(t *testing.T) string {
	t.Helper()
	root := map[string]any{}
	for _, tk := range findTokens {
		node := root
		parts := strings.Split(tk.path, ".")
		for _, seg := range parts[:len(parts)-1] {
			next, ok := node[seg].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[seg] = next
			}
			node = next
		}
		ext := map[string]any{"tier": tk.tier}
		if tk.component != "" {
			ext["component"] = tk.component
		}
		if tk.retired {
			ext["retired"] = true
		}
		leaf := map[string]any{
			"$type": tk.typ, "$value": tk.value,
			"$extensions": map[string]any{"com.c65llc.airframe": ext},
		}
		if tk.description != "" {
			leaf["$description"] = tk.description
		}
		last := parts[len(parts)-1]
		if existing, ok := node[last].(map[string]any); ok { // gap and gap.large
			for k, v := range leaf {
				existing[k] = v
			}
		} else {
			node[last] = leaf
		}
	}
	body, err := json.MarshalIndent(root, "", "  ")
	require.NoError(t, err)
	dir := t.TempDir()
	files := map[string]string{
		"airframe.yaml":                        findConfig,
		"design-system/tokens/all.tokens.json": string(body) + "\n",
		"design-system/components/button.yaml": "id: button\nnoun: Button\nruling: \"@ruling:button\"\ntokens: [button-bg, button-gap]\nowners: [\"web/button/**\"]\n",
		"design-system/components/card.yaml":   "id: card\nnoun: Card\nruling: \"@ruling:card\"\ntokens: [card-bg, card-gap]\nowners: [\"web/card/**\"]\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

func TestFindRanking(t *testing.T) {
	dir := findProject(t)
	var got strings.Builder
	for _, q := range []string{"muted text", "card background", "accent ink", "gap", "danger", "the radius of a corner"} {
		code, stdout, stderr := run("--root", dir, "find", q)
		require.Equal(t, ExitOK, code, "%s: %s", q, stderr)
		assert.Empty(t, stderr)
		fmt.Fprintf(&got, "== %s\n%s\n", q, stdout)
	}
	golden(t, "find.ranking.golden", got.String())

	// Review focus 3: the semantic token outranks the primitive.
	_, stdout, _ := run("--root", dir, "find", "muted text")
	lines := strings.Split(stdout, "\n")
	assert.True(t, strings.HasPrefix(lines[0], "text-muted  "), lines[0])
	assert.Less(t, strings.Index(stdout, "text-muted  "), strings.Index(stdout, "\ngray-500  "))
}

func TestFindLimit(t *testing.T) {
	dir := findProject(t)
	code, stdout, stderr := run("--root", dir, "find", "--limit", "2", "muted text")
	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, 2, strings.Count(stdout, "\n"))
	code, _, stderr = run("--root", dir, "find", "--limit", "0", "gap")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "--limit")
}

func TestFindFileFilter(t *testing.T) {
	dir := findProject(t)
	t.Chdir(dir) // --file is relative to the working directory
	var got strings.Builder
	for _, f := range []string{"web/button/Button.tsx", "web/card/Card.tsx", "web/other/Page.tsx"} {
		code, stdout, stderr := run("--root", dir, "find", "--file", f, "--limit", "50", "gap background accent muted gray")
		require.Equal(t, ExitOK, code, "%s: %s", f, stderr)
		fmt.Fprintf(&got, "== %s\n%s\n", f, stdout)
		for _, line := range strings.Split(stdout, "\n") {
			name, _, _ := strings.Cut(line, "  ")
			// Never a primitive, never another component's token.
			assert.NotContains(t, []string{"gray-500", "red-600", "blue-600", "space-2", "space-4", "radius-2"}, name, f)
			if strings.HasPrefix(name, "card-") {
				assert.Equal(t, "web/card/Card.tsx", f)
			}
			if strings.HasPrefix(name, "button-") {
				assert.Equal(t, "web/button/Button.tsx", f)
			}
		}
	}
	golden(t, "find.file.golden", got.String())
	assert.Contains(t, got.String(), "component button: owners include web/button/**")
	assert.Contains(t, got.String(), "semantic: consumers may read")

	// The path may be spelled with ./ ; the same hits come back.
	_, a, _ := run("--root", dir, "find", "--file", "web/card/Card.tsx", "gap")
	_, b, _ := run("--root", dir, "find", "--file", "./web/card/Card.tsx", "gap")
	assert.Equal(t, a, b)

	// gray names only a primitive, which no file may read.
	code, stdout, stderr := run("--root", dir, "find", "--file", "web/other/Page.tsx", "gray")
	assert.Equal(t, ExitFindings, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "airframe find: 1 token matches \"gray\" but web/other/Page.tsx may not read it (gray-500: primitive: consumers may not read)\n", stderr)
}

func TestFindFileFilteredOutCount(t *testing.T) {
	dir := findProject(t)
	t.Chdir(dir)
	// Only button's two tokens match, and a card file may read neither.
	code, stdout, stderr := run("find", "--file", "web/card/Card.tsx", "button")
	assert.Equal(t, ExitFindings, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "airframe find: 2 tokens match \"button\" but web/card/Card.tsx may read none of them (button-bg: component button: owners (web/button/**) do not match web/card/Card.tsx)\n", stderr)
}

func TestFindFileResolvedAgainstCwd(t *testing.T) {
	dir := findProject(t)
	want := func() string {
		t.Chdir(dir)
		code, stdout, stderr := run("find", "--file", "web/card/Card.tsx", "gap")
		require.Equal(t, ExitOK, code, stderr)
		require.Contains(t, stdout, "card-gap")
		return stdout
	}()

	// From a subdirectory, a relative path names the file from there.
	sub := filepath.Join(dir, "web", "card")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)
	code, stdout, stderr := run("find", "--file", "Card.tsx", "gap")
	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, want, stdout)
	code, stdout, stderr = run("--root", dir, "find", "--file", "./Card.tsx", "gap")
	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, want, stdout)

	// An absolute path works from anywhere.
	t.Chdir(t.TempDir())
	code, stdout, stderr = run("--root", dir, "find", "--file", filepath.Join(dir, "web", "card", "Card.tsx"), "gap")
	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, want, stdout)

	// A path outside the project root is a usage error.
	code, stdout, stderr = run("--root", dir, "find", "--file", "elsewhere/Card.tsx", "gap")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "is outside the project root")
	t.Chdir(dir)
	code, _, stderr = run("find", "--file", "../Card.tsx", "gap")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "is outside the project root")
}

func TestFindTypeFilter(t *testing.T) {
	dir := findProject(t)
	code, stdout, stderr := run("--root", dir, "find", "--type", "dimension", "--limit", "50", "gap spacing background")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "gap  semantic  dimension")
	assert.NotContains(t, stdout, "  color  ")
	code, stdout, _ = run("--root", dir, "find", "--type", "shadow", "gap")
	assert.Equal(t, ExitFindings, code)
	assert.Empty(t, stdout)
}

func TestFindRetiredExcluded(t *testing.T) {
	dir := findProject(t)
	_, stdout, _ := run("--root", dir, "find", "legacy muted")
	assert.NotContains(t, stdout, "text-legacy-muted")
	code, stdout, stderr := run("--root", dir, "find", "--retired", "legacy muted")
	require.Equal(t, ExitOK, code, stderr)
	assert.True(t, strings.HasPrefix(stdout, "text-legacy-muted  "), stdout)
}

func TestFindNoHits(t *testing.T) {
	dir := findProject(t)
	code, stdout, stderr := run("--root", dir, "find", "zzzzqq")
	assert.Equal(t, ExitFindings, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "no token matches")

	code, stdout, _ = run("--root", dir, "find", "--json", "zzzzqq")
	assert.Equal(t, ExitFindings, code)
	assert.Equal(t, "[]\n", stdout)

	// A near miss is offered.
	code, _, stderr = run("--root", dir, "find", "gaap")
	assert.Equal(t, ExitFindings, code)
	assert.Contains(t, stderr, "did you mean gap?")

	// Only stopwords, or no query, is a usage error.
	code, _, _ = run("--root", dir, "find", "the of")
	assert.Equal(t, ExitUsage, code)
	code, _, _ = run("--root", dir, "find")
	assert.Equal(t, ExitUsage, code)
}

func TestFindJSONSchema(t *testing.T) {
	dir := findProject(t)
	t.Chdir(dir)
	code, stdout, stderr := run("--root", dir, "find", "--json", "--file", "web/button/Button.tsx", "gap accent muted")
	require.Equal(t, ExitOK, code, stderr)
	golden(t, "find.json.golden", stdout)
	var doc []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.NotEmpty(t, doc)
	assert.Empty(t, schema.Validate("find", anySlice(doc)))
	for _, h := range doc {
		assert.Contains(t, h, "why")
	}

	// The schema is served, and rejects a hit without a score.
	code, served, _ := run("schema", "find")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, served, "airframe find --json")
	assert.NotEmpty(t, schema.Validate("find", []any{map[string]any{"name": "x"}}))
}

func anySlice(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func TestFindDeterministic(t *testing.T) {
	dir := findProject(t)
	_, first, _ := run("--root", dir, "find", "--json", "--limit", "50", "ink background gap")
	for range 5 {
		_, again, _ := run("--root", dir, "find", "--json", "--limit", "50", "ink background gap")
		assert.Equal(t, first, again)
	}
}
