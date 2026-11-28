// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/find"
	"github.com/c65llc/airframe/internal/gittest"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/report"
	"github.com/c65llc/airframe/internal/specimen"
	"github.com/c65llc/airframe/internal/tokendiff"
)

// loaded is the project in root as the CLI loads it.
func loaded(t *testing.T, root *os.Root) (*project.Project, []diag.Diagnostic) {
	t.Helper()
	p, ds, err := project.Load(os.DirFS(root.Name()))
	require.NoError(t, err)
	require.NotNil(t, p)
	return p, ds
}

// cliJSON is v encoded as the CLI encodes its --json output, as a fence
// holds it: HTML-escaped, without the final newline.
func cliJSON(t *testing.T, v any) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	require.NoError(t, enc.Encode(v))
	return inFence(b.String())
}

// inFence is a document the CLI prints as a fence holds it.
func inFence(doc string) string {
	var b bytes.Buffer
	json.HTMLEscape(&b, []byte(doc))
	return strings.TrimSuffix(b.String(), "\n")
}

func TestToolFind(t *testing.T) {
	root := fixture(t, "consumers")
	p, _ := loaded(t, root)

	got := ok(t, root, "airframe_find", map[string]any{"query": "surface"}, nil)
	assert.Equal(t, cliJSON(t, find.Rank(p, "surface", find.Options{})), got, "byte for byte the find --json document")

	var hits []find.Hit
	ok(t, root, "airframe_find", map[string]any{"query": "surface", "limit": 1}, &hits)
	assert.Len(t, hits, 1)

	got = ok(t, root, "airframe_find", map[string]any{"query": "button", "file": "web/src/button/Button.tsx", "type": "color"}, &hits)
	assert.Equal(t, cliJSON(t, find.Rank(p, "button", find.Options{File: "web/src/button/Button.tsx", Type: "color"})), got)
	require.NotEmpty(t, hits)
	assert.NotEmpty(t, hits[0].Why, "file says why each hit may be read")

	got = ok(t, root, "airframe_find", map[string]any{"query": "surface", "retired": true}, nil)
	assert.Equal(t, cliJSON(t, find.Rank(p, "surface", find.Options{Retired: true})), got)

	// No hits is an empty list, as find --json prints.
	assert.Equal(t, "[]", ok(t, root, "airframe_find", map[string]any{"query": "zebra"}, nil))

	assert.Contains(t, fails(t, root, "airframe_find", map[string]any{"query": "the of"}), `the query "the of" has no words to search for`)
	assert.Contains(t, fails(t, root, "airframe_find", map[string]any{}), "query is required")
	for _, limit := range []any{0, -1} {
		assert.Contains(t, fails(t, root, "airframe_find", map[string]any{"query": "surface", "limit": limit}), "limit must be a positive number")
	}
	assert.Contains(t, fails(t, root, "airframe_find", map[string]any{"query": "surface", "limit": 1.5}), "limit has the wrong type")
}

func TestToolShow(t *testing.T) {
	root := fixture(t, "consumers")
	p, _ := loaded(t, root)

	var tok map[string]any
	got := ok(t, root, "airframe_show", map[string]any{"name": "surface"}, &tok)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, tok["recordVersion"])
	assert.Equal(t, tokenVersion(p, p.Tokens.Tokens["surface"]), tok["recordVersion"])
	want := strings.TrimSuffix(cliJSON(t, report.DescribeToken(p.Tokens, p.Tokens.Tokens["surface"])), "\n}")
	assert.Equal(t, want+",\n  \"recordVersion\": \""+tok["recordVersion"].(string)+"\"\n}", got,
		"show --json, byte for byte, with recordVersion last")

	// A DTCG dotted path names the token too.
	var byPath map[string]any
	ok(t, root, "airframe_show", map[string]any{"name": "button.bg"}, &byPath)
	assert.Equal(t, "button-bg", byPath["name"])

	var comp map[string]any
	got = ok(t, root, "airframe_show", map[string]any{"name": "button"}, &comp)
	assert.Equal(t, "button", comp["id"])
	assert.Equal(t, componentVersion(p, p.Components["button"]), comp["recordVersion"])
	c, err := report.DescribeComponent(p, p.Components["button"])
	require.NoError(t, err)
	want = strings.TrimSuffix(cliJSON(t, c), "\n}")
	assert.Equal(t, want+",\n  \"recordVersion\": \""+comp["recordVersion"].(string)+"\"\n}", got)
	ok(t, root, "airframe_show", map[string]any{"name": "button", "component": true}, &comp)
	assert.Equal(t, "button", comp["id"])

	assert.Equal(t, `unknown token or component "surfac"; airframe_find finds tokens`,
		fails(t, root, "airframe_show", map[string]any{"name": "surfac"}))
	assert.Equal(t, `unknown component "surface"; components are the files in <content>/components`,
		fails(t, root, "airframe_show", map[string]any{"name": "surface", "component": true}))
	assert.Contains(t, fails(t, root, "airframe_show", map[string]any{}), "name is required")
}

func TestToolUsages(t *testing.T) {
	root := fixture(t, "consumers")
	p, _ := loaded(t, root)

	got := ok(t, root, "airframe_usages", map[string]any{"token": "button-bg"}, nil)
	u, err := report.DescribeUsages(p, p.Tokens.Tokens["button-bg"], "")
	require.NoError(t, err)
	assert.Equal(t, cliJSON(t, u), got)
	require.NotEmpty(t, u.References)

	var web report.Usages
	ok(t, root, "airframe_usages", map[string]any{"token": "button-bg", "platform": "web"}, &web)
	require.NotEmpty(t, web.References)
	for _, r := range web.References {
		assert.Equal(t, "web", r.Platform)
	}
	assert.Less(t, len(web.References), len(u.References))

	assert.Equal(t, `unknown platform "watch"; airframe.yaml names the platforms`,
		fails(t, root, "airframe_usages", map[string]any{"token": "button-bg", "platform": "watch"}))
	assert.Equal(t, `unknown token "nope"; airframe_find finds tokens`,
		fails(t, root, "airframe_usages", map[string]any{"token": "nope"}))
}

func TestToolCanRead(t *testing.T) {
	root := fixture(t, "consumers")
	p, _ := loaded(t, root)

	type canRead struct {
		Allowed bool   `json:"allowed"`
		Why     string `json:"why"`
	}
	for file, allowed := range map[string]bool{"web/src/button/Button.tsx": true, "web/src/app.css": false} {
		var got canRead
		text := ok(t, root, "airframe_can_read", map[string]any{"file": file, "token": "button-bg"}, &got)
		wantOK, wantWhy := lint.CanRead(p, file, p.Tokens.Tokens["button-bg"])
		assert.Equal(t, canRead{wantOK, wantWhy}, got, file)
		assert.Equal(t, allowed, got.Allowed, file)
		assert.Equal(t, cliJSON(t, got), text)
	}
	assert.Equal(t, `unknown token "nope"; airframe_find finds tokens`,
		fails(t, root, "airframe_can_read", map[string]any{"file": "web/src/app.css", "token": "nope"}))
	assert.Contains(t, fails(t, root, "airframe_can_read", map[string]any{"token": "surface"}), "file is required")
}

func TestToolLint(t *testing.T) {
	root := fixture(t, "consumers")
	p, ds := loaded(t, root)
	var want bytes.Buffer
	require.NoError(t, diag.WriteJSON(&want, report.Lint(p, ds, lint.Options{}).Diagnostics))
	var doc struct{ Errors []diag.Diagnostic }
	assert.Equal(t, inFence(want.String()), ok(t, root, "airframe_lint", nil, &doc))
	assert.NotEmpty(t, doc.Errors, "the fixture has findings")

	// A fresh read on every call: fix the project, and lint sees it.
	clean := fixture(t, "minimal")
	assert.JSONEq(t, `{"version":1,"errors":[],"warnings":[]}`, ok(t, clean, "airframe_lint", map[string]any{}, nil))
}

const diffConfig = "version: 1\ncontent: ds\nplatforms:\n  web: {emit: css}\n"

func diffTokens(gap int) string {
	return `{"gap": {"$type": "dimension", "$value": {"value": ` + string(rune('0'+gap)) + `, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}}` + "\n"
}

func TestToolDiff(t *testing.T) {
	r := gittest.New(t)
	r.Write(map[string]string{"design/airframe.yaml": diffConfig, "design/ds/tokens/base.tokens.json": diffTokens(4)})
	r.Commit("before")
	r.Git("tag", "v1")
	r.Write(map[string]string{"design/ds/tokens/base.tokens.json": diffTokens(8)})
	r.Commit("after")
	dir := filepath.Join(r.Dir, "design")
	root := openRoot(t, dir)

	want := func(refs ...string) string {
		c, err := report.Diff(dir, os.DirFS(dir), refs, nil)
		require.NoError(t, err)
		var b bytes.Buffer
		require.NoError(t, tokendiff.WriteJSON(&b, c.Diff, c.From, c.To))
		return inFence(b.String())
	}
	var doc struct {
		From, To map[string]any
		Changed  []map[string]any
	}
	assert.Equal(t, want("v1", "main"), ok(t, root, "airframe_diff", map[string]any{"base": "v1", "head": "main"}, &doc))
	require.Len(t, doc.Changed, 1)
	assert.Equal(t, "v1", doc.From["ref"])

	// One ref compares with the working tree, read afresh.
	assert.Contains(t, ok(t, root, "airframe_diff", map[string]any{"base": "HEAD"}, &doc), `"workingTree": true`)
	assert.Empty(t, doc.Changed)
	r.Write(map[string]string{"design/ds/tokens/base.tokens.json": diffTokens(6)})
	assert.Equal(t, want("HEAD"), ok(t, root, "airframe_diff", map[string]any{"base": "HEAD"}, &doc))
	assert.Len(t, doc.Changed, 1)

	// Refs go through the CLI's checks: never an option, always a commit.
	assert.Equal(t, `ref "--output=/tmp/x": starts with "-"; refusing to pass it to git`,
		fails(t, root, "airframe_diff", map[string]any{"base": "--output=/tmp/x"}))
	assert.Equal(t, `ref "nope": names no commit in this repository`,
		fails(t, root, "airframe_diff", map[string]any{"base": "v1", "head": "nope"}))
	assert.Contains(t, fails(t, root, "airframe_diff", map[string]any{}), "base is required")

	// A side that does not load says where, not why.
	r.Write(map[string]string{"design/ds/tokens/base.tokens.json": `{"gap": [`})
	assert.Equal(t, "the project does not load at working tree; airframe_lint reports why",
		fails(t, root, "airframe_diff", map[string]any{"base": "HEAD"}))

	// Outside git.
	assert.Equal(t, "the project is not inside a git work tree; airframe_diff compares git refs",
		fails(t, fixture(t, "minimal"), "airframe_diff", map[string]any{"base": "HEAD"}))
}

func TestToolSpecimen(t *testing.T) {
	root := fixture(t, "apple")
	p, _ := loaded(t, root)
	sheets, err := specimen.Render(p)
	require.NoError(t, err)
	for _, sheet := range []string{"colors", "type", "spacing"} {
		r := callTool(t, root, Options{}, "airframe_specimen", map[string]any{"sheet": sheet, "variant": "notes"})
		require.False(t, r.IsError, r.Content[0].Text)
		assert.Empty(t, r.Structured, "an SVG has no structured content")
		assert.Equal(t, string(sheets[sheet+"-notes.svg"]), unfence(t, r, "svg"), sheet)
	}
	assert.Equal(t, `this project draws one colors sheet per fan-out value: pass variant, such as the value in a file name airframe specimen writes`,
		fails(t, root, "airframe_specimen", map[string]any{"sheet": "colors"}))
	assert.Equal(t, `no colors sheet for variant "watch"; pass a fan-out value of this project`,
		fails(t, root, "airframe_specimen", map[string]any{"sheet": "colors", "variant": "watch"}))
	assert.Equal(t, `invalid sheet "icons"; use colors, type or spacing`,
		fails(t, root, "airframe_specimen", map[string]any{"sheet": "icons"}))

	// A project that does not fan out has one set, and no variant.
	flat := fixture(t, "minimal")
	fp, _ := loaded(t, flat)
	flatSheets, err := specimen.Render(fp)
	require.NoError(t, err)
	r := callTool(t, flat, Options{}, "airframe_specimen", map[string]any{"sheet": "spacing"})
	require.False(t, r.IsError, r.Content[0].Text)
	assert.Equal(t, string(flatSheets["spacing.svg"]), unfence(t, r, "svg"))
	assert.Equal(t, `no spacing sheet for variant "notes"; this project does not fan out, so leave variant out`,
		fails(t, flat, "airframe_specimen", map[string]any{"sheet": "spacing", "variant": "notes"}))

	// As the CLI: a project with load errors draws nothing.
	write(t, flat, "design-system/tokens/broken.tokens.json", "{")
	assert.Equal(t, "the project has load errors, so no sheet was drawn; airframe_lint reports them: fix them, then retry",
		fails(t, flat, "airframe_specimen", map[string]any{"sheet": "spacing"}))
}

func TestToolRules(t *testing.T) {
	assert.Equal(t, cliJSON(t, report.Rules()), ok(t, emptyRoot(t), "airframe_rules", nil, nil), "needs no project")
}

// A file argument is a path from the project root: never absolute, never
// out of it, never with a backslash.
func TestToolPaths(t *testing.T) {
	root := fixture(t, "consumers")
	for _, file := range []string{"../x", "/abs", "/etc/passwd", "web/../../x", "web/src/..", `web\src\app.css`, "C:/x", "c:x", "", ".", "./"} {
		for _, name := range []string{"airframe_find", "airframe_can_read"} {
			args := map[string]any{"query": "surface", "file": file}
			if name == "airframe_can_read" {
				args = map[string]any{"token": "surface", "file": file}
			}
			msg := fails(t, root, name, args)
			assert.Contains(t, msg, "a path from the project root", "%s %q", name, file)
		}
	}
	// A path that stays inside is cleaned, as the CLI's --file is.
	for _, file := range []string{"./web/src/button/Button.tsx", "web//src/button/Button.tsx", "web/src/./button/Button.tsx"} {
		var got struct{ Allowed bool }
		ok(t, root, "airframe_can_read", map[string]any{"token": "button-bg", "file": file}, &got)
		assert.True(t, got.Allowed, file)
	}
}
