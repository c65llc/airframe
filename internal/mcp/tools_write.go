// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/ojson"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/report"
	"github.com/c65llc/airframe/internal/tokens"
)

// writeTools are the write tools, in the order tools/list gives them,
// after the read tools. --read-only leaves them out and refuses them.
var writeTools = []tool{
	{
		name: "airframe_token_set", title: "Create or update a token", write: true,
		description: "Set or remove fields of one token, or create one. Call it with preview: true first. Each key of set is set to its value, and null removes it: $value, $type, $description, tier, component, emit, retired, density.<value>, modes.<mode key> and names.<format>. A mode value goes where lint loads it: beside the token's existing value for that key, else into the project's mode file for the key; a new mode file is made only where the project's layout says where it goes. The edit is formatted as airframe fmt formats it; it is refused, writing nothing, if the project would not load or build afterwards. A write also regenerates the outputs airframe build writes. The result lists the source files written, the outputs regenerated, the token's new recordVersion and the project's lint report. Renaming is not an edit: use airframe rename.",
		input: object([]string{"name", "set"}, map[string]any{
			"name":        str("The token name, with - or the DTCG path's . (accent-bg or accent.bg). With create, the new token's name."),
			"set":         map[string]any{"type": "object", "description": "Keys to set, such as {\"$value\": \"{gray.700}\", \"modes.appearance:dark\": \"{gray.200}\", \"$description\": null}. A null value removes the key."},
			"create":      boolean("Add a new token instead of editing one: give path (default: the name) and file."),
			"path":        str("With create: the token's DTCG path, dotted, such as gap.md for gap-md inside the gap group."),
			"file":        str("With create: the base token file it goes in, as a path from the project root under the content directory, ending .tokens.json. A new file is made."),
			"preview":     previewProperty,
			"format":      formatProperty,
			"baseVersion": baseVersionProperty,
		}),
		run: tokenSet,
	},
	{
		name: "airframe_component_set", title: "Create or update a component", write: true,
		description: "Set or remove fields of one component file, <content>/components/<id>.yaml, or create one. Call it with preview: true first. Each key of set is set to its value, and null removes it: noun, ruling (@ruling:<id>), tokens, owners, implementations.<platform> (a file, or \"n/a: reason\"), hostedBy and shared. Other keys and comments stay. The file is formatted as airframe fmt formats it; the edit is refused, writing nothing, if the project would not load or build afterwards. The result lists the files written, the component's new recordVersion and the project's lint report.",
		input: object([]string{"id", "set"}, map[string]any{
			"id":          str("The component ID, its file name without .yaml, such as tab-strip."),
			"set":         map[string]any{"type": "object", "description": "Keys to set, such as {\"noun\": \"Tab strip\", \"implementations.web\": \"web/src/TabStrip.tsx\"}. A null value removes the key. Not id."},
			"create":      boolean("Add a new component file instead of editing one."),
			"preview":     previewProperty,
			"format":      formatProperty,
			"baseVersion": baseVersionProperty,
		}),
		run: componentSet,
	},
}

// --- JSON argument values ----------------------------------------------------

// bom is the UTF-8 byte order mark, which a token file may start with.
var bom = []byte{0xEF, 0xBB, 0xBF}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// stringValue is raw as a string, or an error naming key.
func stringValue(key string, raw json.RawMessage) (string, error) {
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", failf("%s must be a string, or null to remove it", key)
	}
	return v, nil
}

func boolValue(key string, raw json.RawMessage) (bool, error) {
	var v bool
	if json.Unmarshal(raw, &v) != nil {
		return false, failf("%s must be true or false, or null to remove it", key)
	}
	return v, nil
}

func stringsValue(key string, raw json.RawMessage) ([]string, error) {
	var v []string
	if json.Unmarshal(raw, &v) != nil {
		return nil, failf("%s must be an array of strings, or null to remove it", key)
	}
	if v == nil {
		v = []string{}
	}
	return v, nil
}

// --- ojson editing -------------------------------------------------------------

// setField sets key in object n, in place when it is there, else last.
func setField(n *ojson.Node, key string, v *ojson.Node) {
	if _, ok := n.Fields[key]; !ok {
		n.Keys = append(n.Keys, key)
	}
	n.Fields[key] = v
}

func delField(n *ojson.Node, key string) {
	if _, ok := n.Fields[key]; !ok {
		return
	}
	delete(n.Fields, key)
	n.Keys = slices.DeleteFunc(n.Keys, func(k string) bool { return k == key })
}

func newObject() *ojson.Node {
	return &ojson.Node{Kind: ojson.Object, Fields: map[string]*ojson.Node{}}
}

// child is n's object child key, made (last) when create is set and it is
// not there; nil when it is missing or not an object.
func child(n *ojson.Node, key string, create bool) *ojson.Node {
	c, ok := n.Fields[key]
	if !ok && create {
		c = newObject()
		setField(n, key, c)
	}
	if c == nil || c.Kind != ojson.Object {
		return nil
	}
	return c
}

func jsonNode(raw json.RawMessage) (*ojson.Node, error) {
	n, err := ojson.Parse(raw)
	if err != nil {
		return nil, failf("a value in set is not valid JSON")
	}
	return n, nil
}

func stringNode(s string) *ojson.Node { return &ojson.Node{Kind: ojson.String, Str: s} }

// --- airframe_token_set --------------------------------------------------------

// tokenKeys are the keys set takes as they are; densityKey, modesKey and
// namesKey take a suffix.
var tokenKeys = []string{"$value", "$type", "$description", "tier", "component", "emit", "retired"}

const (
	densityKey = "density."
	modesKey   = "modes."
	namesKey   = "names."
)

// checkTokenKey validates one key of set and its value, before anything is
// read.
func checkTokenKey(name, key string, raw json.RawMessage) error {
	switch {
	case key == "name" || key == "path":
		return failf("a token's name is its path, so renaming it is not an edit: run `airframe rename %s <new-name>`, which also rewrites every alias and consumer read", name)
	case key == "$value" && isNull(raw):
		return failf("$value cannot be removed: every token has one. To retire the token, set retired: true")
	case key == "$value":
		_, err := jsonNode(raw)
		return err
	case isNull(raw):
	case key == "retired":
		_, err := boolValue(key, raw)
		return err
	case key == "emit":
		_, err := stringsValue(key, raw)
		return err
	case slices.Contains(tokenKeys, key), strings.HasPrefix(key, namesKey):
		if _, err := stringValue(key, raw); err != nil {
			return err
		}
	}
	for _, prefix := range []string{densityKey, modesKey, namesKey} {
		if key == prefix {
			what := map[string]string{densityKey: "a density value, such as density.compact", modesKey: "a mode key, such as modes.appearance:dark", namesKey: "an emit format, such as names.swift"}[prefix]
			return failf("%q needs %s", key, what)
		}
		if strings.HasPrefix(key, prefix) {
			return nil
		}
	}
	if !slices.Contains(tokenKeys, key) {
		return failf("unknown key %q; set takes $value, $type, $description, tier, component, emit, retired, density.<value>, modes.<mode key> and names.<format>", key)
	}
	return nil
}

// tokenEdit is one token_set in progress: the token files it has parsed
// and changed.
type tokenEdit struct {
	p      *project.Project
	name   string
	path   []string
	file   string                 // the token's base file
	docs   map[string]*ojson.Node // every file read, by path
	modes  map[string]string      // each token file's root mode key, "" for a base file
	wantIn []config.ModeKey       // mode keys the edit sets, which the edited project must have
}

// doc is file parsed, or a new object for a file that does not exist.
func (e *tokenEdit) doc(file string) (*ojson.Node, error) {
	if d, ok := e.docs[file]; ok {
		return d, nil
	}
	d := newObject()
	if src, ok := e.p.Sources[file]; ok {
		var err error
		if d, err = ojson.Parse(bytes.TrimPrefix(src, bom)); err != nil || d.Kind != ojson.Object {
			return nil, failf("a token file the edit needs does not parse; airframe_lint reports why: fix it, then retry")
		}
	}
	e.docs[file] = d
	return d, nil
}

// rootMode is the canonical mode key at the root of token file src, or ""
// for a base file (or one whose mode does not parse).
func rootMode(cfg *config.Config, src []byte) string {
	d, err := ojson.Parse(bytes.TrimPrefix(src, bom))
	if err != nil || d.Kind != ojson.Object {
		return ""
	}
	x := child(d, "$extensions", false)
	if x == nil {
		return ""
	}
	ns := child(x, tokens.Namespace, false)
	if ns == nil || ns.Fields["mode"] == nil || ns.Fields["mode"].Kind != ojson.String {
		return ""
	}
	k, err := cfg.ParseModeKey(ns.Fields["mode"].Str)
	if err != nil {
		return ""
	}
	return string(k)
}

// node is the token's node in file, walking its path; with create, the
// groups on the way are made. A token where a group is wanted is an
// error.
func (e *tokenEdit) node(file string, create bool) (*ojson.Node, error) {
	n, err := e.doc(file)
	if err != nil {
		return nil, err
	}
	for i, key := range e.path {
		c, ok := n.Fields[key]
		if !ok {
			if !create {
				return nil, nil
			}
			c = newObject()
			setField(n, key, c)
		}
		if c.Kind != ojson.Object {
			return nil, failf("%s in that file is not an object, so the token cannot go there", strings.Join(e.path[:i+1], "."))
		}
		if _, isToken := c.Fields["$value"]; isToken && i < len(e.path)-1 {
			return nil, failf("%s is a token, so it cannot hold %s", strings.Join(e.path[:i+1], "."), e.name)
		}
		n = c
	}
	return n, nil
}

// prune removes the token's node from file when it holds nothing, and
// every group on its path that it leaves empty.
func (e *tokenEdit) prune(file string) {
	d := e.docs[file]
	chain := []*ojson.Node{d}
	for _, key := range e.path {
		n := child(chain[len(chain)-1], key, false)
		if n == nil {
			return
		}
		chain = append(chain, n)
	}
	for i := len(chain) - 1; i > 0 && len(chain[i].Keys) == 0; i-- {
		delField(chain[i-1], e.path[i-1])
	}
}

// ext is node's Airframe extension object, made when create is set.
func ext(node *ojson.Node, create bool) *ojson.Node {
	x := child(node, "$extensions", create)
	if x == nil {
		return nil
	}
	return child(x, tokens.Namespace, create)
}

// tidyExt removes an emptied extension object, and an emptied
// $extensions.
func tidyExt(node *ojson.Node) {
	x := child(node, "$extensions", false)
	if x == nil {
		return
	}
	if ns := child(x, tokens.Namespace, false); ns != nil {
		for _, k := range []string{"density", "modes", "names"} {
			if c := child(ns, k, false); c != nil && len(c.Keys) == 0 {
				delField(ns, k)
			}
		}
		if len(ns.Keys) == 0 {
			delField(x, tokens.Namespace)
		}
	}
	if len(x.Keys) == 0 {
		delField(node, "$extensions")
	}
}

// setIn sets key in obj to raw, or removes it for null.
func setIn(obj *ojson.Node, key string, raw json.RawMessage) error {
	if isNull(raw) {
		delField(obj, key)
		return nil
	}
	v, err := jsonNode(raw)
	if err != nil {
		return err
	}
	setField(obj, key, v)
	return nil
}

// apply sets one key on the token's node in its base file.
func (e *tokenEdit) apply(node *ojson.Node, key string, raw json.RawMessage) error {
	switch {
	case strings.HasPrefix(key, "$"):
		return setIn(node, key, raw)
	case strings.HasPrefix(key, modesKey):
		return e.mode(node, strings.TrimPrefix(key, modesKey), raw)
	case strings.HasPrefix(key, densityKey), strings.HasPrefix(key, namesKey):
		group, sub, _ := strings.Cut(key, ".")
		if isNull(raw) {
			if ns := ext(node, false); ns != nil {
				if c := child(ns, group, false); c != nil {
					delField(c, sub)
				}
			}
			return nil
		}
		return setIn(child(ext(node, true), group, true), sub, raw)
	}
	if isNull(raw) {
		if ns := ext(node, false); ns != nil {
			delField(ns, key)
		}
		return nil
	}
	return setIn(ext(node, true), key, raw)
}

// mode sets or removes the token's value for mode key k (as written). The
// value goes where lint loads it: where the token already has one for the
// key (its own modes or density, or a mode file); else the one token file
// whose root mode is the key; else a new file beside the other values'
// files of the same axis, when they are all <dir>/<value>.tokens.json in
// one directory. Anything else is refused: a value written somewhere the
// loader does not read it, or reads it for another key, would be lost.
func (e *tokenEdit) mode(node *ojson.Node, k string, raw json.RawMessage) error {
	mk, err := e.p.Config.ParseModeKey(k)
	if err != nil || mk == "" {
		return failf("modes.%s: not a mode key of this project; airframe.yaml declares the axes and their values", k)
	}
	remove := isNull(raw)
	if !remove {
		e.wantIn = append(e.wantIn, mk)
	}
	// The token's own modes, then its density shorthand.
	if ns := ext(node, false); ns != nil {
		if modes := child(ns, "modes", false); modes != nil {
			for _, written := range modes.Keys {
				if pk, err := e.p.Config.ParseModeKey(written); err == nil && pk == mk {
					return setIn(modes, written, raw)
				}
			}
		}
		if axis, value, ok := strings.Cut(string(mk), ":"); ok && axis == "density" && !strings.Contains(value, "+") {
			if density := child(ns, "density", false); density != nil && density.Fields[value] != nil {
				return setIn(density, value, raw)
			}
		}
	}
	// The mode files that hold the key.
	var holders []string
	for _, f := range e.p.TokenFiles {
		if e.modes[f] == string(mk) {
			holders = append(holders, f)
		}
	}
	for _, f := range holders {
		n, err := e.node(f, false)
		if err != nil {
			return err
		}
		if n != nil && n.Fields["$value"] != nil {
			err := setIn(n, "$value", raw)
			e.prune(f)
			return err
		}
	}
	switch {
	case remove:
		return failf("token %s has no %s value to remove", e.name, mk)
	case len(holders) > 1:
		return failf("more than one token file holds mode %s, so there is no one place for a new value; add it by hand to the file you mean", mk)
	case len(holders) == 1:
		n, err := e.node(holders[0], true)
		if err != nil {
			return err
		}
		return setIn(n, "$value", raw)
	}
	file, ok := e.newModeFile(mk)
	if !ok {
		return failf(`no token file holds mode %s yet, and the project's mode files do not show where one goes; create one under the content directory with {"$extensions": {"%s": {"mode": "%s"}}} at its root, then retry`, mk, tokens.Namespace, mk)
	}
	d, err := e.doc(file)
	if err != nil {
		return err
	}
	ns := newObject()
	setField(ns, "mode", stringNode(string(mk)))
	x := newObject()
	setField(x, tokens.Namespace, ns)
	setField(d, "$extensions", x)
	e.modes[file] = string(mk)
	n, err := e.node(file, true)
	if err != nil {
		return err
	}
	return setIn(n, "$value", raw)
}

// newModeFile is where a new mode file for the one-axis key mk goes: beside
// the files of the axis's other values, when every one of them is
// <dir>/<value>.tokens.json in one directory. ok is false when the layout
// does not say.
func (e *tokenEdit) newModeFile(mk config.ModeKey) (string, bool) {
	axis, value, _ := strings.Cut(string(mk), ":")
	if mk.Pairs() != 1 {
		return "", false
	}
	dir := ""
	for _, f := range e.p.TokenFiles {
		m := e.modes[f]
		a, v, _ := strings.Cut(m, ":")
		if m == "" || a != axis || strings.Contains(m, "+") {
			continue
		}
		if path.Base(f) != v+".tokens.json" || (dir != "" && path.Dir(f) != dir) {
			return "", false
		}
		dir = path.Dir(f)
	}
	if dir == "" {
		return "", false
	}
	file := path.Join(dir, value+".tokens.json")
	if _, exists := e.p.Sources[file]; exists {
		return "", false
	}
	return file, true
}

// tokenFile checks create's file argument: a base token file under the
// content directory.
func tokenFile(p *project.Project, cfgModes map[string]string, file string) (string, error) {
	if file == "" {
		return "", failf("file is required with create: the base token file, such as %s/tokens/semantic.tokens.json", path.Clean(p.Config.Content))
	}
	clean, err := projectFile("file", file)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(clean, path.Clean(p.Config.Content)+"/") || !strings.HasSuffix(clean, ".tokens.json") {
		return "", failf("file %q must be a token file under the content directory: <content>/…/<name>.tokens.json, where the loader reads it", file)
	}
	if cfgModes[clean] != "" {
		return "", failf("file %q is a mode file; a new token goes in a base token file, and its mode values after", file)
	}
	return clean, nil
}

func tokenSet(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		Name        string                     `json:"name"`
		Set         map[string]json.RawMessage `json:"set"`
		Create      bool                       `json:"create"`
		Path        string                     `json:"path"`
		File        string                     `json:"file"`
		Preview     bool                       `json:"preview"`
		Format      string                     `json:"format"`
		BaseVersion string                     `json:"baseVersion"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	if err := checkFormat(a.Preview, a.Format); err != nil {
		return toolResult{}, err
	}
	switch {
	case a.Name == "":
		return toolResult{}, failf("name is required: the token name, such as accent-bg")
	case len(a.Set) == 0:
		return toolResult{}, failf("set is empty: pass the keys to set, such as {\"$value\": \"{gray.700}\"}")
	case !a.Create && (a.Path != "" || a.File != ""):
		return toolResult{}, failf("path and file go with create: true; an existing token stays where it is")
	}
	keys := slices.Sorted(keysOf(a.Set))
	for _, k := range keys {
		if err := checkTokenKey(a.Name, k, a.Set[k]); err != nil {
			return toolResult{}, err
		}
	}
	if a.Create && a.Set["$value"] == nil {
		return toolResult{}, failf("a new token needs $value in set")
	}
	p, _, err := s.load()
	if err != nil {
		return toolResult{}, err
	}
	modes := map[string]string{}
	for _, f := range p.TokenFiles {
		modes[f] = rootMode(p.Config, p.Sources[f])
	}
	e := &tokenEdit{p: p, docs: map[string]*ojson.Node{}, modes: modes}
	t := report.LookupToken(p.Tokens, a.Name)
	now := ""
	if t != nil {
		now = tokenVersion(p, t)
	}
	if err := checkBase(a.BaseVersion, now, "token "+a.Name); err != nil {
		return toolResult{}, err
	}
	var node *ojson.Node
	switch {
	case a.Create && t != nil:
		return toolResult{}, failf("token %q already exists; leave create out to edit it", a.Name)
	case a.Create:
		if !tokens.ValidName(a.Name) {
			return toolResult{}, failf("%q is not a valid token name: lowercase words joined by -, such as surface-raised", a.Name)
		}
		e.name, e.path = a.Name, []string{a.Name}
		if a.Path != "" {
			e.path = strings.Split(a.Path, ".")
			if got := strings.Join(e.path, "-"); got != a.Name {
				return toolResult{}, failf("path %q names token %q, not %q", a.Path, got, a.Name)
			}
		}
		if e.file, err = tokenFile(p, modes, a.File); err != nil {
			return toolResult{}, err
		}
		parent, err := e.parent()
		if err != nil {
			return toolResult{}, err
		}
		if _, taken := parent.Fields[e.path[len(e.path)-1]]; taken {
			return toolResult{}, failf("something is already at path %s in that file", strings.Join(e.path, "."))
		}
		node = newObject()
		setField(parent, e.path[len(e.path)-1], node)
	case t == nil:
		return toolResult{}, failf("unknown token %q; airframe_find finds tokens, and create: true adds one", a.Name)
	default:
		e.name, e.path, e.file = t.Name, t.Path, t.File
		if node, err = e.node(t.File, false); err != nil {
			return toolResult{}, err
		}
		if node == nil {
			return toolResult{}, failf("a token file the edit needs does not parse; airframe_lint reports why: fix it, then retry")
		}
	}
	for _, k := range keys {
		if err := e.apply(node, k, a.Set[k]); err != nil {
			return toolResult{}, err
		}
	}
	tidyExt(node)
	changed := map[string][]byte{}
	for file, d := range e.docs {
		data, err := format.Tokens(ojson.Marshal(d, "  "))
		if err != nil {
			return toolResult{}, failf("the edited token file cannot be written in canonical form; check the values you set")
		}
		changed[file] = data
	}
	name, want := e.name, e.wantIn
	check := func(np *project.Project) string {
		nt := np.Tokens.Tokens[name]
		if nt == nil {
			return "refused: the token would not load where it would be written; nothing was written"
		}
		for _, k := range want {
			if nt.Modes[k] == nil {
				return fmt.Sprintf("refused: the %s value would not load where it would be written; nothing was written", k)
			}
		}
		return ""
	}
	return s.commit(p, changed, a.Preview, a.Format, check, func(np *project.Project) string {
		if nt := np.Tokens.Tokens[name]; nt != nil {
			return tokenVersion(np, nt)
		}
		return ""
	})
}

// parent is the group a new token goes in, made as needed; a token there
// cannot hold another.
func (e *tokenEdit) parent() (*ojson.Node, error) {
	whole := e.path
	e.path = whole[:len(whole)-1]
	defer func() { e.path = whole }()
	n, err := e.node(e.file, true)
	if err == nil && len(e.path) > 0 && n.Fields["$value"] != nil {
		return nil, failf("%s is a token, so it cannot hold %s", strings.Join(e.path, "."), e.name)
	}
	return n, err
}

// keysOf is the keys of m, for sorting.
func keysOf[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// --- airframe_component_set ----------------------------------------------------

// componentID is a new component's ID: lowercase words joined by -.
var componentID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// componentKeyOrder is the order a component's keys take when added, as
// the spec writes a component file.
var componentKeyOrder = []string{"id", "noun", "ruling", "tokens", "owners", "hostedBy", "implementations", "shared"}

func componentRank(key string) int {
	k, _, _ := strings.Cut(key, ".")
	return slices.Index(componentKeyOrder, k)
}

// componentValue validates one key of set and returns its value, nil for a
// removal.
func componentValue(key string, raw json.RawMessage) (any, error) {
	if key == "id" {
		return nil, failf("a component's ID is its file name, so it cannot be set; create a new component instead")
	}
	k, sub, dotted := strings.Cut(key, ".")
	switch {
	case dotted && (k != "implementations" || sub == ""):
		return nil, failf("unknown key %q; set takes noun, ruling, tokens, owners, implementations.<platform>, hostedBy and shared", key)
	case componentRank(key) < 0:
		return nil, failf("unknown key %q; set takes noun, ruling, tokens, owners, implementations.<platform>, hostedBy and shared", key)
	case isNull(raw):
		return nil, nil
	case k == "tokens" || k == "owners":
		return stringsValue(key, raw)
	case k == "shared":
		return boolValue(key, raw)
	case k == "implementations" && !dotted:
		var v map[string]string
		if json.Unmarshal(raw, &v) != nil {
			return nil, failf("implementations must be an object of platform to file (or \"n/a: reason\"), or null to remove it")
		}
		return v, nil
	}
	return stringValue(key, raw)
}

// pair is the index of key's key node in mapping m, or -1.
func pair(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// setYAML sets key in mapping m to v (a removal when v is nil). A
// replaced list or map keeps the flow or block style it had.
func setYAML(m *yaml.Node, key string, v any) error {
	i := pair(m, key)
	if v == nil {
		if i >= 0 {
			m.Content = slices.Delete(m.Content, i, i+2)
		}
		return nil
	}
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return err
	}
	if i >= 0 {
		if old := m.Content[i+1]; old.Kind == n.Kind && n.Kind != yaml.ScalarNode {
			n.Style = old.Style
		}
		m.Content[i+1] = &n
		return nil
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &n)
	return nil
}

func componentSet(s *server, raw json.RawMessage) (toolResult, error) {
	var a struct {
		ID          string                     `json:"id"`
		Set         map[string]json.RawMessage `json:"set"`
		Create      bool                       `json:"create"`
		Preview     bool                       `json:"preview"`
		Format      string                     `json:"format"`
		BaseVersion string                     `json:"baseVersion"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResult{}, err
	}
	if err := checkFormat(a.Preview, a.Format); err != nil {
		return toolResult{}, err
	}
	switch {
	case a.ID == "":
		return toolResult{}, failf("id is required: the component ID, such as tab-strip")
	case len(a.Set) == 0:
		return toolResult{}, failf("set is empty: pass the keys to set, such as {\"noun\": \"Tab strip\"}")
	case !componentID.MatchString(a.ID):
		return toolResult{}, failf("invalid component ID %q: lowercase words joined by -, such as tab-strip", a.ID)
	}
	keys := slices.SortedFunc(keysOf(a.Set), func(x, y string) int {
		if d := componentRank(x) - componentRank(y); d != 0 {
			return d
		}
		return strings.Compare(x, y)
	})
	values := map[string]any{}
	for _, k := range keys {
		v, err := componentValue(k, a.Set[k])
		if err != nil {
			return toolResult{}, err
		}
		values[k] = v
	}
	p, _, err := s.load()
	if err != nil {
		return toolResult{}, err
	}
	file := path.Join(path.Clean(p.Config.Content), "components", a.ID+".yaml")
	exists := slices.Contains(p.ComponentFiles, file)
	if err := checkBase(a.BaseVersion, fileVersion(p, file, exists), "component "+a.ID); err != nil {
		return toolResult{}, err
	}
	var doc yaml.Node
	switch {
	case a.Create && exists:
		return toolResult{}, failf("component %q already exists; leave create out to edit it", a.ID)
	case a.Create:
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		_ = setYAML(doc.Content[0], "id", a.ID) // a string always encodes
	case !exists:
		return toolResult{}, failf("unknown component %q; pass create: true to add one", a.ID)
	default:
		if yaml.Unmarshal(p.Sources[file], &doc) != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
			return toolResult{}, failf("the component file does not parse; airframe_lint reports why: fix it, then retry")
		}
	}
	m := doc.Content[0]
	for _, k := range keys {
		target, key := m, k
		if sub, ok := strings.CutPrefix(k, "implementations."); ok {
			target, key = nil, sub
			if i := pair(m, "implementations"); i >= 0 && m.Content[i+1].Kind == yaml.MappingNode {
				target = m.Content[i+1]
			} else if values[k] != nil {
				if err := setYAML(m, "implementations", map[string]string{}); err != nil {
					return toolResult{}, err
				}
				target = m.Content[pair(m, "implementations")+1]
				target.Style = 0
			}
			if target == nil {
				continue // removing from a map that is not there
			}
		}
		if err := setYAML(target, key, values[k]); err != nil {
			return toolResult{}, failf("the edited component file cannot be written in canonical form; check the values you set")
		}
	}
	if i := pair(m, "implementations"); i >= 0 && m.Content[i+1].Kind == yaml.MappingNode && len(m.Content[i+1].Content) == 0 {
		m.Content = slices.Delete(m.Content, i, i+2)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return toolResult{}, failf("the edited component file cannot be written in canonical form; check the values you set")
	}
	_ = enc.Close()
	data, err := format.YAML(buf.Bytes())
	if err != nil {
		return toolResult{}, failf("the edited component file cannot be written in canonical form; check the values you set")
	}
	id := a.ID
	return s.commit(p, map[string][]byte{file: data}, a.Preview, a.Format, nil, func(np *project.Project) string {
		if c := np.Components[id]; c != nil {
			return componentVersion(np, c)
		}
		return ""
	})
}

// fileVersion is the version of the component file, "" when there is none.
func fileVersion(p *project.Project, file string, exists bool) string {
	if !exists {
		return ""
	}
	return componentVersion(p, &component.Component{File: file})
}
