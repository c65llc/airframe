// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpResult is one tools/call result: its fenced text and its structured
// content.
type mcpResult struct {
	Text       string
	Structured json.RawMessage
}

// mcpCalls runs airframe mcp in dir over the given tools/call requests
// (name and arguments) and returns each result.
func mcpCalls(t *testing.T, dir string, calls ...string) []mcpResult {
	t.Helper()
	in := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"
	for i, c := range calls {
		in += `{"jsonrpc":"2.0","id":` + strconv.Itoa(i+1) + `,"method":"tools/call","params":` + c + "}\n"
	}
	var stdout, stderr bytes.Buffer
	require.Equal(t, ExitOK, Main([]string{"--root", dir, "mcp"}, strings.NewReader(in), &stdout, &stderr), stderr.String())
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	require.Len(t, lines, len(calls)+1)
	var results []mcpResult
	for _, l := range lines[1:] {
		var r struct {
			Result struct {
				Content    []struct{ Text string }
				Structured json.RawMessage `json:"structuredContent"`
				IsError    bool
			}
		}
		require.NoError(t, json.Unmarshal([]byte(l), &r))
		require.False(t, r.Result.IsError, l)
		results = append(results, mcpResult{Text: r.Result.Content[0].Text, Structured: r.Result.Structured})
	}
	return results
}

// fencedJSON is a command's --json output as an MCP result fences it.
func fencedJSON(out string) string {
	var b bytes.Buffer
	json.HTMLEscape(&b, []byte(out))
	return "<untrusted-data source=\"airframe-project\" kind=\"json\">\n" + strings.TrimSuffix(b.String(), "\n") + "\n</untrusted-data>"
}

// Each read tool returns what its command prints: the fenced text holds
// the command's --json bytes, and structuredContent is the same document.
func TestMCPMatchesCLI(t *testing.T) {
	dir := usagesProject(t)
	res := mcpCalls(t, dir,
		`{"name":"airframe_find","arguments":{"query":"button","file":"web/src/button/Button.tsx"}}`,
		`{"name":"airframe_show","arguments":{"name":"surface"}}`,
		`{"name":"airframe_show","arguments":{"name":"button"}}`,
		`{"name":"airframe_usages","arguments":{"token":"button-bg","platform":"web"}}`,
		`{"name":"airframe_lint"}`,
		`{"name":"airframe_rules"}`,
	)
	for i, args := range [][]string{
		{"find", "button", "--file", filepath.Join(dir, "web/src/button/Button.tsx"), "--json"},
		{"show", "surface", "--json"},
		{"show", "button", "--json"},
		{"usages", "button-bg", "--platform", "web", "--json"},
		{"lint", "--json"},
		{"rules", "--json"},
	} {
		_, out, _ := run(append([]string{"--root", dir}, args...)...)
		require.NotEmpty(t, out, args)
		want := fencedJSON(out)
		if args[0] == "show" {
			// show's object, with the record's version after its keys.
			var doc map[string]any
			require.NoError(t, json.Unmarshal(res[i].Structured, &doc))
			version, _ := doc["recordVersion"].(string)
			require.Regexp(t, `^sha256:[0-9a-f]{64}$`, version, args)
			want = fencedJSON(strings.TrimSuffix(out, "\n}\n") + ",\n  \"recordVersion\": \"" + version + "\"\n}\n")
			delete(doc, "recordVersion")
			strip, err := json.Marshal(doc)
			require.NoError(t, err)
			assert.JSONEq(t, out, string(strip), args)
		} else if args[0] == "find" {
			assert.JSONEq(t, `{"hits":`+out+`}`, string(res[i].Structured), "structuredContent is an object: find's array is its hits")
		} else {
			assert.JSONEq(t, out, string(res[i].Structured), args)
		}
		assert.Equal(t, want, res[i].Text, args)
	}

	// specimen's sheets, as SVG text, are the files the command writes.
	apple := t.TempDir()
	require.NoError(t, os.CopyFS(apple, os.DirFS("../../testdata/apple")))
	res = mcpCalls(t, apple, `{"name":"airframe_specimen","arguments":{"sheet":"colors","variant":"notes"}}`)
	code, _, errOut := run("--root", apple, "specimen")
	require.Equal(t, ExitOK, code, errOut)
	svg, err := os.ReadFile(filepath.Join(apple, "design-system/generated/specimen/colors-notes.svg"))
	require.NoError(t, err)
	assert.Equal(t, "<untrusted-data source=\"airframe-project\" kind=\"svg\">\n"+string(svg)+"\n</untrusted-data>", res[0].Text)
}

func TestMCPDiffMatchesCLI(t *testing.T) {
	_, dir, _, _ := diffRepo(t)
	res := mcpCalls(t, dir, `{"name":"airframe_diff","arguments":{"base":"v1","head":"main"}}`, `{"name":"airframe_diff","arguments":{"base":"v1"}}`)
	for i, args := range [][]string{{"v1", "main"}, {"v1"}} {
		code, out, errOut := run(append([]string{"--root", dir, "diff", "--format", "json"}, args...)...)
		require.Equal(t, ExitOK, code, errOut)
		assert.JSONEq(t, out, string(res[i].Structured), args)
		assert.Equal(t, fencedJSON(out), res[i].Text, args)
	}
}

// A write through MCP leaves exactly what airframe fmt and airframe build
// would: fmt --check and build --check are clean afterwards.
func TestMCPWriteMatchesFmtAndBuild(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS("../../testdata/minimal")))
	mcpCalls(t, dir,
		`{"name":"airframe_token_set","arguments":{"name":"surface","set":{"$value":"{gray.200}","$description":"The page."}}}`,
		`{"name":"airframe_token_set","arguments":{"name":"gap-md","create":true,"path":"gap.md","file":"design-system/tokens/semantic.tokens.json","set":{"$value":"{space.1}","tier":"semantic"}}}`,
		`{"name":"airframe_token_set","arguments":{"name":"gap-md","set":{"modes.appearance:dark":"{space.5}"}}}`,
		`{"name":"airframe_component_set","arguments":{"id":"button","create":true,"set":{"noun":"Button","tokens":["button-bg","button-ink","button-target","button-gap"],"owners":["web/**"]}}}`,
	)
	for _, args := range [][]string{{"fmt", "--check"}, {"build", "--check"}} {
		code, out, errOut := run(append([]string{"--root", dir}, args...)...)
		assert.Equal(t, ExitOK, code, args, out+errOut)
		assert.Empty(t, out, args)
	}
	_, out, _ := run("--root", dir, "show", "gap-md", "--json")
	assert.Contains(t, out, `"key": "appearance:dark"`)
}

func TestMCPCommand(t *testing.T) {
	dir := usagesProject(t)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"--root", dir, "mcp", "--read-only"}, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), &stdout, &stderr)
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, stdout.String(), `"name":"airframe_find"`)
	assert.NotContains(t, stdout.String(), `"name":"airframe_token_set"`, "--read-only hides the write tools")
	assert.NotContains(t, stdout.String(), `"name":"airframe_component_set"`)
	assert.Regexp(t, `^airframe mcp: serving .+ \(read-only\)\n$`, stderr.String())

	// A fresh read on every call: an edit between calls is seen.
	stdout.Reset()
	stderr.Reset()
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"airframe_show","arguments":{"name":"surface"}}}` + "\n"
	code = Main([]string{"--root", dir, "mcp"}, strings.NewReader(in), &stdout, &stderr)
	assert.Equal(t, ExitOK, code, stderr.String())
	assert.Regexp(t, `^airframe mcp: serving .+\nairframe mcp: session=[0-9a-f]{8} client="unknown" tool=airframe_show class=read preview=false outcome=ok ms=\d+ bytes=\d+\n$`, stderr.String())

	code, _, errOut := run("--root", dir, "mcp", "extra")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "mcp takes no arguments")

	// No project: exit 2 before serving.
	code, out, errOut := run("--root", t.TempDir(), "mcp")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "no airframe.yaml in")
	assert.NotContains(t, errOut, "serving")

	// Stdin closing at once is a clean end.
	code, out, _ = run("--root", dir, "mcp")
	assert.Equal(t, ExitOK, code)
	assert.Empty(t, out)

	// Losing stdout ends the server with status 2.
	stderr.Reset()
	code = Main([]string{"--root", dir, "mcp"}, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), failWriter{}, &stderr)
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr.String(), "broken pipe")
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
