// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"bytes"
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
)

// tool is one MCP tool. A write tool is left out in read-only mode.
type tool struct {
	name, title, description string
	input                    map[string]any // the JSON Schema of the arguments
	write                    bool
	run                      func(s *server, args json.RawMessage) (toolResult, error)
}

// toolInfo is a tool as tools/list describes it.
type toolInfo struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
}

// Schema helpers: an object of properties, some required, no others.
func object(required []string, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enum(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func boolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func integer(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description, "minimum": 1}
}

// tools is every tool, in the order tools/list gives them: the read tools
// (tools_read.go), then the write tools (tools_write.go).
var tools = slices.Concat(readTools, writeTools)

// ToolSpec is a tool's wire name, its one-line summary and whether it
// writes, for the tests that keep the shipped skills in step with the
// server.
type ToolSpec struct {
	Name    string
	Summary string
	Writes  bool
}

// Tools lists every tool the server can offer, in tools/list order.
func Tools() []ToolSpec {
	out := make([]ToolSpec, len(tools))
	for i, t := range tools {
		out[i] = ToolSpec{Name: t.name, Summary: t.title, Writes: t.write}
	}
	return out
}

// listTools describes the tools the session offers.
func (s *server) listTools() []toolInfo {
	out := []toolInfo{}
	for _, t := range tools {
		if t.write && s.opts.ReadOnly {
			continue
		}
		ann := map[string]any{"title": t.title, "readOnlyHint": !t.write, "openWorldHint": false}
		if t.write {
			// Every write replaces what was there.
			ann["destructiveHint"] = true
			ann["idempotentHint"] = true
		}
		out = append(out, toolInfo{Name: t.name, Title: t.title, Description: t.description, InputSchema: t.input, Annotations: ann})
	}
	return out
}

// tool finds the tool name among those the session offers.
func (s *server) tool(name string) (tool, bool) {
	i := slices.IndexFunc(tools, func(t tool) bool { return t.name == name })
	if i < 0 || (tools[i].write && s.opts.ReadOnly) {
		return tool{}, false
	}
	return tools[i], true
}

// decodeArgs decodes a tool's arguments into v, refusing unknown ones so a
// misspelt argument fails instead of being ignored.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return argsError(err)
	}
	return nil
}

// encodeJSON encodes v as the CLI encodes its --json reports.
func encodeJSON(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // the results are plain structs, maps and slices, which always encode
	return b.Bytes()
}

// load reads the project, refusing one that did not load at all (the
// diagnostics then hold only why). A partial load is returned, as the CLI
// uses it, with its diagnostics.
func (s *server) load() (*project.Project, []diag.Diagnostic, error) {
	p, ds, err := project.Load(s.fsys)
	if err != nil {
		return nil, nil, err
	}
	if p == nil {
		switch {
		case slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Rule == "config-missing" }):
			return nil, nil, failf("no airframe.yaml in the project directory; run `airframe init` there, then restart the server")
		case slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Rule == "no-tokens" }):
			return nil, nil, failf("the project has no token files yet; airframe_lint reports where they go")
		}
		return nil, nil, failf("airframe.yaml does not load; airframe_lint reports why: fix it, then retry")
	}
	return p, ds, nil
}

// hasErrors reports whether any of ds is an error.
func hasErrors(ds []diag.Diagnostic) bool {
	return slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Severity == diag.Error })
}

// projectFile checks a file argument, named arg, and returns it as a clean
// slash path from the project root. The server has no working directory,
// so a path is always from the root; one that is absolute, has a drive
// letter or a backslash, or climbs out with .. is refused, never resolved.
func projectFile(arg, file string) (string, error) {
	const want = "give a path from the project root, such as web/src/App.tsx"
	switch {
	case strings.TrimSpace(file) == "":
		return "", failf("%s is required: %s", arg, want)
	case strings.Contains(file, `\`):
		return "", failf("%s %q has a backslash: %s, with forward slashes", arg, file, want)
	case strings.HasPrefix(file, "/"):
		return "", failf("%s %q is absolute: %s", arg, file, want)
	case len(file) >= 2 && file[1] == ':' && (file[0] >= 'A' && file[0] <= 'Z' || file[0] >= 'a' && file[0] <= 'z'):
		return "", failf("%s %q has a drive letter: %s", arg, file, want)
	case slices.Contains(strings.Split(file, "/"), ".."):
		return "", failf("%s %q climbs out with ..: %s", arg, file, want)
	}
	clean := path.Clean(file)
	if clean == "." {
		return "", failf("%s %q names the root itself: %s", arg, file, want)
	}
	return clean, nil
}
