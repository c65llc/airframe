// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokens

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/ojson"
)

var (
	nameRE  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	aliasRE = regexp.MustCompile(`^\{([^{}]+)\}$`)
	bom     = []byte{0xEF, 0xBB, 0xBF}
)

// supported lists the $types Airframe understands.
var supported = map[string]bool{
	"color": true, "dimension": true, "number": true, "fontFamily": true,
	"fontWeight": true, "duration": true, "cubicBezier": true, "shadow": true,
}

// ValidName reports whether name passes the token-name rule: lowercase
// words of letters and digits joined by "-".
func ValidName(name string) bool { return nameRE.MatchString(name) }

// NamePattern is the token-name rule as a regular expression, for messages.
func NamePattern() string { return nameRE.String() }

func sortedKeys[K ~string, V any](m map[K]V) []K { return slices.Sorted(maps.Keys(m)) }

// decl is one token declaration found in a file, not yet merged into the Set.
type decl struct {
	file     string
	fileMode string // the file root's mode key as written; "" for a base file
	name     string
	path     []string
	node     *ojson.Node
	typ      string // own or inherited $type; "" when neither is given
}

type parser struct {
	cfg  *config.Config
	set  *Set
	base map[string]bool // tokens with a declaration in a base file
	out  []diag.Diagnostic
}

func (p *parser) report(rule, file, path string, line int, format string, a ...any) {
	p.out = append(p.out, diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: file, Path: path, Line: line,
		Message: fmt.Sprintf(format, a...), Docs: diag.DocsURL(rule),
	})
}

func (p *parser) invalid(file, path string, line int, format string, a ...any) {
	p.report("schema-invalid", file, path, line, format, a...)
}

// Parse reads every tokens file (keyed by path, parsed in path order) into
// one Set. Base files are merged before mode files, so a mode file's
// overrides attach to tokens declared anywhere. Problems are reported as
// diagnostics; the Set holds whatever could be read.
func Parse(cfg *config.Config, files map[string][]byte) (*Set, []diag.Diagnostic) {
	p := &parser{cfg: cfg, base: map[string]bool{}, set: &Set{
		Tokens: map[string]*Token{},
		axes:   cfg.Axes,
		origin: map[*Value]string{},
	}}
	var decls []decl
	for _, file := range sortedKeys(files) {
		decls = append(decls, p.file(file, files[file])...)
	}
	for _, d := range decls {
		if d.fileMode == "" {
			p.merge(d)
		}
	}
	for _, d := range decls {
		if d.fileMode != "" {
			p.merge(d)
		}
	}
	seen := map[string]bool{}
	for _, d := range decls {
		if _, ok := p.set.Tokens[d.name]; ok && !seen[d.name] {
			seen[d.name] = true
			p.set.Order = append(p.set.Order, d.name)
		}
	}
	for _, name := range p.set.Order {
		if t := p.set.Tokens[name]; !p.base[name] {
			p.invalid(t.File, strings.Join(t.Path, "."), t.Line, "token %q has mode values but no $value", name)
		}
	}
	return p.set, p.out
}

// file parses one tokens file and returns its declarations.
func (p *parser) file(file string, data []byte) []decl {
	root, err := ojson.Parse(bytes.TrimPrefix(data, bom))
	if err != nil {
		var se *ojson.SyntaxError
		line := 0
		msg := err.Error()
		if errors.As(err, &se) {
			line, msg = se.Line, se.Msg
		}
		p.report("json-syntax", file, "", line, "%s", msg)
		return nil
	}
	if root.Kind != ojson.Object {
		p.invalid(file, "", root.Line, "a tokens file must be a JSON object")
		return nil
	}
	mode, typ, ok := p.group(file, root, nil, "", true)
	if !ok {
		return nil
	}
	var out []decl
	p.walk(file, mode, root, nil, typ, &out)
	return out
}

// group checks the $-properties of a group (the file root when root is
// set) and returns the root's file-level mode and the group's $type, which
// falls back to inherited. ok is false when the file cannot be used.
func (p *parser) group(file string, n *ojson.Node, path []string, inherited string, root bool) (mode, typ string, ok bool) {
	typ, ok = inherited, true
	for _, k := range n.Keys {
		v := n.Fields[k]
		at := joinPath(path, k)
		switch k {
		case "$type":
			if v.Kind != ojson.String {
				p.invalid(file, at, v.Line, "$type must be a string")
				continue
			}
			typ = v.Str
		case "$description":
			if v.Kind != ojson.String {
				p.invalid(file, at, v.Line, "$description must be a string")
			}
		case "$deprecated":
		case "$extensions":
			if v.Kind != ojson.Object {
				p.invalid(file, at, v.Line, "$extensions must be an object")
				continue
			}
			ext, has := v.Fields[Namespace]
			if !has {
				continue
			}
			extAt := joinPath(path, k, Namespace)
			if !root {
				p.invalid(file, extAt, ext.Line, "%s is not allowed on a group; put it on each token", Namespace)
				continue
			}
			m, good := p.rootExt(file, extAt, ext)
			mode, ok = m, ok && good
		default:
			if strings.HasPrefix(k, "$") {
				p.invalid(file, at, v.Line, "unknown key %q", k)
			}
		}
	}
	return mode, typ, ok
}

// rootExt reads the Airframe extension on a file root: only "mode" is
// allowed. It reports and returns ok=false when the mode is unusable.
func (p *parser) rootExt(file, at string, ext *ojson.Node) (string, bool) {
	if ext.Kind != ojson.Object {
		p.invalid(file, at, ext.Line, "%s must be an object", Namespace)
		return "", false
	}
	ok := true
	mode := ""
	for _, k := range ext.Keys {
		v := ext.Fields[k]
		if k != "mode" {
			p.invalid(file, at+"."+k, v.Line, "unknown key %q at the file root (only \"mode\" is allowed)", k)
			continue
		}
		if v.Kind != ojson.String || strings.TrimSpace(v.Str) == "" {
			p.invalid(file, at+".mode", v.Line, "mode must be a non-empty mode key")
			ok = false
			continue
		}
		if _, err := p.cfg.ParseModeKey(v.Str); err != nil {
			p.report("mode-unknown", file, at+".mode", v.Line, "mode key %q: %v", v.Str, err)
			ok = false
			continue
		}
		mode = v.Str
	}
	return mode, ok
}

// walk collects the tokens under group n.
func (p *parser) walk(file, mode string, n *ojson.Node, path []string, typ string, out *[]decl) {
	for _, k := range n.Keys {
		if strings.HasPrefix(k, "$") {
			continue
		}
		child := n.Fields[k]
		cp := append(slices.Clone(path), k)
		if child.Kind != ojson.Object {
			p.invalid(file, joinPath(cp), child.Line, "%q must be a token or a group (an object)", k)
			continue
		}
		if _, isToken := child.Fields["$value"]; !isToken {
			_, gt, _ := p.group(file, child, cp, typ, false)
			p.walk(file, mode, child, cp, gt, out)
			continue
		}
		name := strings.Join(cp, "-")
		if !nameRE.MatchString(name) {
			p.report("token-name-invalid", file, joinPath(cp), child.Line,
				"token name %q must match %s (lowercase words joined by -)", name, nameRE)
			continue
		}
		own := typ
		if t, ok := child.Fields["$type"]; ok && t.Kind == ojson.String {
			own = t.Str
		}
		*out = append(*out, decl{file: file, fileMode: mode, name: name, path: cp, node: child, typ: own})
	}
}

// ext is a token's Airframe extension fields as read.
type ext struct {
	tier, component, chrome string
	shared, retired         bool
	emit                    []string
	exempt                  map[string]string
	names                   map[string]string
	modes                   *ojson.Node
	density                 *ojson.Node
}

// readExt reads a token's $extensions[Namespace], reporting bad fields and
// skipping them.
func (p *parser) readExt(file, at string, n *ojson.Node) ext {
	var e ext
	if n.Kind != ojson.Object {
		p.invalid(file, at, n.Line, "%s must be an object", Namespace)
		return e
	}
	str := func(k string, v *ojson.Node) string {
		if v.Kind != ojson.String {
			p.invalid(file, at+"."+k, v.Line, "%s must be a string", k)
			return ""
		}
		return v.Str
	}
	boolean := func(k string, v *ojson.Node) bool {
		if v.Kind != ojson.Bool {
			p.invalid(file, at+"."+k, v.Line, "%s must be true or false", k)
			return false
		}
		return v.Bool
	}
	object := func(k string, v *ojson.Node) *ojson.Node {
		if v.Kind != ojson.Object {
			p.invalid(file, at+"."+k, v.Line, "%s must be an object", k)
			return nil
		}
		return v
	}
	for _, k := range n.Keys {
		v := n.Fields[k]
		switch k {
		case "tier":
			e.tier = str(k, v)
		case "component":
			e.component = str(k, v)
		case "chrome":
			e.chrome = str(k, v)
		case "shared":
			e.shared = boolean(k, v)
		case "retired":
			e.retired = boolean(k, v)
		case "emit":
			if v.Kind != ojson.Array {
				p.invalid(file, at+".emit", v.Line, "emit must be an array of strings")
				continue
			}
			for _, it := range v.Items {
				if it.Kind != ojson.String {
					p.invalid(file, at+".emit", it.Line, "emit must be an array of strings")
					continue
				}
				e.emit = append(e.emit, it.Str)
			}
		case "exempt":
			o := object(k, v)
			if o == nil {
				continue
			}
			for _, rule := range o.Keys {
				r := o.Fields[rule]
				// An empty reason is kept: lint reports it as
				// exemption-without-reason, and it suppresses nothing.
				if r.Kind != ojson.String {
					p.invalid(file, at+".exempt."+rule, r.Line, "exemption from %s needs a reason string", rule)
					continue
				}
				if e.exempt == nil {
					e.exempt = map[string]string{}
				}
				e.exempt[rule] = r.Str
			}
		case "names":
			o := object(k, v)
			if o == nil {
				continue
			}
			for _, f := range o.Keys {
				nv := o.Fields[f]
				if !slices.Contains(config.Formats, f) {
					p.invalid(file, at+".names."+f, nv.Line, "names key %q is not an emit format; known: %s", f, strings.Join(config.Formats, ", "))
					continue
				}
				if nv.Kind != ojson.String || nv.Str == "" {
					p.invalid(file, at+".names."+f, nv.Line, "names.%s must be a non-empty string", f)
					continue
				}
				if e.names == nil {
					e.names = map[string]string{}
				}
				e.names[f] = nv.Str
			}
		case "modes":
			e.modes = object(k, v)
		case "density":
			e.density = object(k, v)
		case "mode":
			p.invalid(file, at+".mode", v.Line, "mode is only allowed at the file root; use modes on a token")
		default:
			p.invalid(file, at+"."+k, v.Line, "unknown key %q", k)
		}
	}
	return e
}

// combine joins two mode keys as written.
func combine(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "+" + b
}

// rawValue is one value of a declaration before it is parsed.
type rawValue struct {
	key     string // as written, already combined with the file mode
	node    *ojson.Node
	at      string
	density string // the density value, for a density shorthand entry
}

// merge adds one declaration to the Set.
func (p *parser) merge(d decl) {
	at := joinPath(d.path)
	tok := p.set.Tokens[d.name]
	typ := d.typ
	if tok != nil {
		switch {
		case typ == "":
			typ = tok.Type
		case typ != tok.Type:
			p.invalid(d.file, at, d.node.Line, "token %q is %s here but %s in %s", d.name, typ, tok.Type, tok.File)
			return
		}
	}
	if typ == "" {
		p.invalid(d.file, at, d.node.Line, "token %q has no $type (set one on the token or a parent group)", d.name)
		return
	}
	if !supported[typ] {
		p.report("type-unsupported", d.file, at, d.node.Line, "token %q has $type %q; supported: %s",
			d.name, typ, strings.Join(sortedKeys(supported), ", "))
		return
	}

	var e ext
	for _, k := range d.node.Keys {
		v := d.node.Fields[k]
		switch k {
		case "$value", "$type", "$deprecated":
			if k == "$type" && v.Kind != ojson.String {
				p.invalid(d.file, at+".$type", v.Line, "$type must be a string")
			}
		case "$description":
			if v.Kind != ojson.String {
				p.invalid(d.file, at+".$description", v.Line, "$description must be a string")
			}
		case "$extensions":
			if v.Kind != ojson.Object {
				p.invalid(d.file, at+".$extensions", v.Line, "$extensions must be an object")
			} else if x, ok := v.Fields[Namespace]; ok {
				e = p.readExt(d.file, at+".$extensions."+Namespace, x)
			}
		default:
			p.invalid(d.file, at+"."+k, v.Line, "unknown key %q in a token (a token cannot contain other tokens)", k)
		}
	}

	if tok == nil {
		tok = &Token{
			Name: d.name, Path: d.path, Type: typ, Tier: e.tier, Component: e.component,
			Chrome: e.chrome, Shared: e.shared, Retired: e.retired, Exempt: e.exempt,
			Emit: e.emit, Names: e.names, File: d.file, Line: d.node.Line, Modes: map[config.ModeKey]*Value{},
		}
		if desc, ok := d.node.Fields["$description"]; ok && desc.Kind == ojson.String {
			tok.Description = desc.Str
		}
		p.set.Tokens[d.name] = tok
	} else {
		p.overrideOnly(d, at)
	}
	if d.fileMode == "" {
		p.base[d.name] = true
	}

	raws := []rawValue{{key: d.fileMode, node: d.node.Fields["$value"], at: at + ".$value"}}
	extAt := at + ".$extensions." + Namespace
	if e.modes != nil {
		for _, k := range e.modes.Keys {
			raws = append(raws, rawValue{key: combine(d.fileMode, k), node: e.modes.Fields[k], at: extAt + ".modes." + k})
		}
	}
	if e.density != nil {
		for _, k := range e.density.Keys {
			raws = append(raws, rawValue{key: combine(d.fileMode, "density:"+k), node: e.density.Fields[k], at: extAt + ".density." + k, density: k})
		}
	}

	unit := ""
	var own *Value // this declaration's $value
	for _, r := range raws {
		key, err := p.cfg.ParseModeKey(r.key)
		if err != nil {
			p.report("mode-unknown", d.file, r.at, r.node.Line, "mode key %q: %v", r.key, err)
			continue
		}
		// The density shorthand may list the base density when it equals
		// $value (a full table reads better); it is dropped. A differing
		// one would contradict $value, the base render.
		_, _, densityBase := p.cfg.BasePair(config.ModeKey("density:" + r.density))
		densityBase = densityBase && r.density != ""
		axis, val, base := p.cfg.BasePair(key)
		if base && !densityBase {
			p.report("mode-base-value", d.file, r.at, r.node.Line, "%s:%s is the base %s; write the key without it", axis, val, axis)
			continue
		}
		hint := ""
		if r.density != "" {
			hint = unit
			if hint == "" {
				hint = unitOf(tok.Default)
			}
		}
		v, serr := parseValue(r.node, typ, hint, r.at)
		if serr != nil {
			p.invalid(d.file, serr.path, serr.line, "%s", serr.msg)
			continue
		}
		if densityBase {
			if own == nil || !sameValue(v, own) {
				p.report("mode-base-value", d.file, r.at, r.node.Line,
					"density:%s is the base density, which $value already is; drop it or make it equal $value", r.density)
			}
			continue
		}
		if r.at == at+".$value" {
			unit = unitOf(v)
			own = v
		}
		p.put(tok, key, v, d.file, at)
	}
}

// metaKeys are the extension fields that describe a token rather than
// give it a value; only its first declaration may set them.
var metaKeys = []string{"tier", "component", "chrome", "shared", "retired", "emit", "exempt", "names"}

// overrideOnly reports each descriptive field on a later declaration of a
// token that already exists: those fields would otherwise be dropped.
func (p *parser) overrideOnly(d decl, at string) {
	flag := func(path string, line int) {
		p.invalid(d.file, path, line, "only $value, modes and density may appear on an override of %q", d.name)
	}
	if v, ok := d.node.Fields["$description"]; ok {
		flag(at+".$description", v.Line)
	}
	exts, ok := d.node.Fields["$extensions"]
	if !ok || exts.Kind != ojson.Object {
		return
	}
	x, ok := exts.Fields[Namespace]
	if !ok || x.Kind != ojson.Object {
		return
	}
	for _, k := range x.Keys {
		if slices.Contains(metaKeys, k) {
			flag(at+".$extensions."+Namespace+"."+k, x.Fields[k].Line)
		}
	}
}

// put stores v under key, reporting a second definition of the same key.
func (p *parser) put(tok *Token, key config.ModeKey, v *Value, file, at string) {
	prev := tok.Default
	if key != "" {
		prev = tok.Modes[key]
	}
	if prev != nil {
		what := "mode key " + string(key)
		if key == "" {
			what = "$value"
		}
		p.report("mode-override-conflict", file, at, v.Line,
			"token %q defines %s twice: %s:%d and %s:%d",
			tok.Name, what, p.set.origin[prev], prev.Line, file, v.Line)
		return
	}
	p.set.origin[v] = file
	if key == "" {
		tok.Default = v
	} else {
		tok.Modes[key] = v
	}
}

func joinPath(path []string, more ...string) string {
	return strings.Join(append(slices.Clone(path), more...), ".")
}
