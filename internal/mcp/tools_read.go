// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/find"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/report"
	"github.com/c65llc/airframe/internal/specimen"
	"github.com/c65llc/airframe/internal/tokendiff"
	"github.com/c65llc/airframe/internal/tokens"
)

// The read tools return what the matching command prints with --json,
// byte for byte, built by the same code (internal/report, find, lint,
// tokendiff, specimen): an agent and a person see one answer.

// fileProperty is the argument naming a consumer file.
var fileProperty = str("A consumer file, as a path from the project root such as web/src/button/Button.tsx: never absolute, never with .. or a backslash.")

// readTools are the read tools, in the order tools/list gives them.
var readTools = []tool{
	{
		name: "airframe_find", title: "Find tokens",
		description: "Rank the tokens that match a free-text query by their names, descriptions and components, as airframe find --json does: an array of {name, tier, type, value, description, score, why}, best first, empty when nothing matches. Use it to find the role token before styling anything. With file, keep only the tokens that file may read, each with why.",
		input: object([]string{"query"}, map[string]any{
			"query":   str("What the token is for, in words, such as \"muted text on cards\"."),
			"file":    fileProperty,
			"type":    str("Keep only tokens of this DTCG type, such as color or dimension."),
			"limit":   integer("Return at most this many hits. Default: 10."),
			"retired": boolean("Include retired tokens. Default: false."),
		}),
		run: findTool,
	},
	{
		name: "airframe_show", title: "Show a token or a component",
		description: "One token as airframe show --json prints it (name, tier, component, type, description, and each value per mode key with its alias chain), or, for a component, its ruling, tokens, owners, implementations, hosts and consumer files. A name that is both is the token unless component is true. The result adds recordVersion, the version of the record's source, to pass back as baseVersion when editing it.",
		input: object([]string{"name"}, map[string]any{
			"name":      str("A token name, with - or the DTCG path's . (accent-bg or accent.bg), or a component ID."),
			"component": boolean("Show the component of that name, even when a token has it too."),
		}),
		run: showTool,
	},
	{
		name: "airframe_usages", title: "List a token's usages",
		description: "Every consumer read of a token, direct or through a wrapper (platform, file, line, col, the text read, and for a component token whether the file is one of its owners), and the tokens whose values alias it, as airframe usages --json prints them.",
		input: object([]string{"token"}, map[string]any{
			"token":    str("The token name, such as accent-bg."),
			"platform": str("List only the reads in this configured platform, such as web."),
		}),
		run: usagesTool,
	},
	{
		name: "airframe_can_read", title: "Check a token read",
		description: "Whether a consumer file may read a token under the project's tier and owner rules, and why: {\"allowed\": true|false, \"why\": \"…\"}. Check before adding a read; lint reports a read that is not allowed.",
		input: object([]string{"file", "token"}, map[string]any{
			"file":  fileProperty,
			"token": str("The token name, such as button-bg."),
		}),
		run: canReadTool,
	},
	{
		name: "airframe_lint", title: "Lint the project",
		description: "Check the project as airframe lint --json does and return its report: {\"version\":1,\"errors\":[…],\"warnings\":[…]}, each finding with rule, severity, file, line, message and docs. A project that does not load reports why. Act on the rule, not the wording; airframe_rules lists the rules.",
		input:       object(nil, map[string]any{}),
		run:         lintTool,
	},
	{
		name: "airframe_diff", title: "Diff the tokens between git refs",
		description: "How the tokens changed from git ref base to ref head, or to the working tree when head is left out, as airframe diff --format json prints it: {version, from, to, added, removed, renamed, changed}. It reads git's objects and checks nothing out. A ref that starts with - is refused.",
		input: object([]string{"base"}, map[string]any{
			"base": str("The git ref to compare from, such as main, v1.2.0 or HEAD~1."),
			"head": str("The git ref to compare to. Default: the working tree."),
		}),
		run: diffTool,
	},
	{
		name: "airframe_specimen", title: "Draw a specimen sheet",
		description: "One of the colour, type and spacing sheets airframe specimen writes, as SVG text, the same bytes. A project whose platforms fan out (one output per value of a mode axis) draws one set per value: pass variant, that value.",
		input: object([]string{"sheet"}, map[string]any{
			"sheet":   enum("The sheet to draw.", "colors", "type", "spacing"),
			"variant": str("The fan-out value whose sheet to draw, as in the file name colors-<variant>.svg. Leave it out for a project that does not fan out."),
		}),
		run: specimenTool,
	},
	{
		name: "airframe_rules", title: "List lint rules",
		description: "Every rule ID as airframe rules --json lists it: {\"version\":1,\"rules\":[…]}, each with its kind (check, load or build), default severity, summary and documentation page.",
		input:       object(nil, map[string]any{}),
		run:         rulesTool,
	},
}

// lookup finds the token name, refusing one there is none of. The message
// suggests no near name: a name is project content, and errors are not
// fenced.
func lookup(p *project.Project, name string) (*tokens.Token, error) {
	t := report.LookupToken(p.Tokens, name)
	if t == nil {
		return nil, failf("unknown token %q; airframe_find finds tokens", name)
	}
	return t, nil
}

func findTool(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		Query   string  `json:"query"`
		File    *string `json:"file"`
		Type    string  `json:"type"`
		Limit   *int    `json:"limit"`
		Retired bool    `json:"retired"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	switch {
	case strings.TrimSpace(a.Query) == "":
		return toolResult{}, failf("query is required: what the token is for, in words")
	case len(find.Words(a.Query)) == 0:
		return toolResult{}, failf("the query %q has no words to search for", a.Query)
	case a.Limit != nil && *a.Limit < 1:
		return toolResult{}, failf("limit must be a positive number, not %d", *a.Limit)
	}
	opts := find.Options{Type: a.Type, Retired: a.Retired}
	if a.Limit != nil {
		opts.Limit = *a.Limit
	}
	if a.File != nil {
		file, err := projectFile("file", *a.File)
		if err != nil {
			return toolResult{}, err
		}
		opts.File = file
	}
	p, _, err := s.load()
	if err != nil {
		return toolResult{}, err
	}
	hits := find.Rank(p, a.Query, opts)
	if hits == nil {
		hits = []find.Hit{} // find --json prints [] for none
	}
	return fencedJSON(encodeJSON(hits)), nil
}

func showTool(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		Name      string `json:"name"`
		Component bool   `json:"component"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	if a.Name == "" {
		return toolResult{}, failf("name is required: a token name or a component ID")
	}
	p, _, err := s.load()
	if err != nil {
		return toolResult{}, err
	}
	c := p.Components[a.Name]
	var t *tokens.Token
	if !a.Component {
		t = report.LookupToken(p.Tokens, a.Name)
	}
	switch {
	case t != nil:
		// show --json's object, with the record's version after its keys.
		return fencedJSON(encodeJSON(struct {
			report.Token
			RecordVersion string `json:"recordVersion"`
		}{report.DescribeToken(p.Tokens, t), tokenVersion(p, t)})), nil
	case c != nil:
		shown, err := report.DescribeComponent(p, c)
		if err != nil {
			return toolResult{}, err
		}
		return fencedJSON(encodeJSON(struct {
			report.Component
			RecordVersion string `json:"recordVersion"`
		}{shown, componentVersion(p, c)})), nil
	case a.Component:
		return toolResult{}, failf("unknown component %q; components are the files in <content>/components", a.Name)
	}
	return toolResult{}, failf("unknown token or component %q; airframe_find finds tokens", a.Name)
}

func usagesTool(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	if a.Token == "" {
		return toolResult{}, failf("token is required: the token name, such as accent-bg")
	}
	p, _, err := s.load()
	if err != nil {
		return toolResult{}, err
	}
	t, err := lookup(p, a.Token)
	if err != nil {
		return toolResult{}, err
	}
	if a.Platform != "" && !slices.ContainsFunc(p.Config.Platforms, func(pl config.Platform) bool { return pl.Name == a.Platform }) {
		return toolResult{}, failf("unknown platform %q; airframe.yaml names the platforms", a.Platform)
	}
	u, err := report.DescribeUsages(p, t, a.Platform)
	if err != nil {
		return toolResult{}, err
	}
	return fencedJSON(encodeJSON(u)), nil
}

func canReadTool(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		File  string `json:"file"`
		Token string `json:"token"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	file, err := projectFile("file", a.File)
	if err != nil {
		return toolResult{}, err
	}
	if a.Token == "" {
		return toolResult{}, failf("token is required: the token name, such as button-bg")
	}
	p, _, err := s.load()
	if err != nil {
		return toolResult{}, err
	}
	t, err := lookup(p, a.Token)
	if err != nil {
		return toolResult{}, err
	}
	allowed, why := lint.CanRead(p, file, t)
	return fencedJSON(encodeJSON(struct {
		Allowed bool   `json:"allowed"`
		Why     string `json:"why"`
	}{allowed, why})), nil
}

func lintTool(s *server, raw json.RawMessage) (toolResult, error) {
	if err := decodeArgs(raw, &struct{}{}); err != nil {
		return toolResult{}, err
	}
	// As lint --json: a project that did not load reports why.
	p, ds, err := project.Load(s.fsys)
	if err != nil {
		return toolResult{}, err
	}
	res := report.Lint(p, ds, lint.Options{})
	if res.Err != nil { // an I/O failure: the findings are incomplete
		return toolResult{}, res.Err
	}
	var b bytes.Buffer
	_ = diag.WriteJSON(&b, res.Diagnostics) // a bytes.Buffer never fails a write
	return fencedJSON(b.Bytes()), nil
}

func diffTool(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		Base string  `json:"base"`
		Head *string `json:"head"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	if a.Base == "" {
		return toolResult{}, failf("base is required: the git ref to compare from, such as main")
	}
	refs := []string{a.Base}
	if a.Head != nil {
		refs = append(refs, *a.Head) // an empty head is refused by the ref checks, as the CLI refuses it
	}
	c, err := report.Diff(s.dir, s.fsys, refs, nil)
	if err != nil {
		return toolResult{}, err
	}
	var b bytes.Buffer
	_ = tokendiff.WriteJSON(&b, c.Diff, c.From, c.To) // a bytes.Buffer never fails a write
	return fencedJSON(b.Bytes()), nil
}

// sheets are the specimen sheets, as their files begin.
var sheets = []string{"colors", "type", "spacing"}

func specimenTool(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		Sheet   string `json:"sheet"`
		Variant string `json:"variant"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	if !slices.Contains(sheets, a.Sheet) {
		return toolResult{}, failf("invalid sheet %q; use colors, type or spacing", a.Sheet)
	}
	p, ds, err := s.load()
	if err != nil {
		return toolResult{}, err
	}
	// As the CLI: drawing a partly read project would leave tokens out.
	if hasErrors(ds) {
		return toolResult{}, failf("the project has load errors, so no sheet was drawn; airframe_lint reports them: fix them, then retry")
	}
	drawn, err := specimen.Render(p)
	if err != nil {
		return toolResult{}, err
	}
	name := a.Sheet + ".svg"
	if a.Variant != "" {
		name = a.Sheet + "-" + a.Variant + ".svg"
	}
	if svg, ok := drawn[name]; ok {
		return fencedSVG(string(svg)), nil
	}
	_, flat := drawn[a.Sheet+".svg"]
	switch {
	case a.Variant == "":
		return toolResult{}, failf("this project draws one %s sheet per fan-out value: pass variant, such as the value in a file name airframe specimen writes", a.Sheet)
	case flat:
		return toolResult{}, failf("no %s sheet for variant %q; this project does not fan out, so leave variant out", a.Sheet, a.Variant)
	}
	return toolResult{}, failf("no %s sheet for variant %q; pass a fan-out value of this project", a.Sheet, a.Variant)
}

func rulesTool(_ *server, raw json.RawMessage) (toolResult, error) {
	if err := decodeArgs(raw, &struct{}{}); err != nil {
		return toolResult{}, err
	}
	return fencedJSON(encodeJSON(report.Rules())), nil
}
