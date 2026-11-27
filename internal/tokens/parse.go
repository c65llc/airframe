// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokens

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
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

// fontWeights maps the DTCG font-weight keywords to their numeric weights.
var fontWeights = map[string]float64{
	"thin": 100, "hairline": 100,
	"extra-light": 200, "ultra-light": 200,
	"light":  300,
	"normal": 400, "regular": 400, "book": 400,
	"medium":    500,
	"semi-bold": 600, "demi-bold": 600,
	"bold":       700,
	"extra-bold": 800, "ultra-bold": 800,
	"black": 900, "heavy": 900,
	"extra-black": 950, "ultra-black": 950,
}

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
	cfg *config.Config
	set *Set
	out []diag.Diagnostic
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
	p := &parser{cfg: cfg, set: &Set{
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
				if r.Kind != ojson.String || strings.TrimSpace(r.Str) == "" {
					p.invalid(file, at+".exempt."+rule, r.Line, "exemption from %s needs a non-empty reason", rule)
					continue
				}
				if e.exempt == nil {
					e.exempt = map[string]string{}
				}
				e.exempt[rule] = r.Str
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
	density bool
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
			Emit: e.emit, File: d.file, Line: d.node.Line, Modes: map[config.ModeKey]*Value{},
		}
		if desc, ok := d.node.Fields["$description"]; ok && desc.Kind == ojson.String {
			tok.Description = desc.Str
		}
		p.set.Tokens[d.name] = tok
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
			raws = append(raws, rawValue{key: combine(d.fileMode, "density:"+k), node: e.density.Fields[k], at: extAt + ".density." + k, density: true})
		}
	}

	unit := ""
	for _, r := range raws {
		key, err := p.cfg.ParseModeKey(r.key)
		if err != nil {
			p.report("mode-unknown", d.file, r.at, r.node.Line, "mode key %q: %v", r.key, err)
			continue
		}
		hint := ""
		if r.density {
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
		if r.at == at+".$value" {
			unit = unitOf(v)
		}
		p.put(tok, key, v, d.file, at)
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

// unitOf is the unit of a dimension or duration literal, or "".
func unitOf(v *Value) string {
	if v == nil {
		return ""
	}
	switch l := v.Lit.(type) {
	case Dim:
		return l.Unit
	case Duration:
		return l.Unit
	}
	return ""
}

func joinPath(path []string, more ...string) string {
	return strings.Join(append(slices.Clone(path), more...), ".")
}

// shapeErr is a value that does not have the shape its $type needs.
type shapeErr struct {
	path, msg string
	line      int
}

func bad(n *ojson.Node, path, format string, a ...any) *shapeErr {
	return &shapeErr{path: path, line: n.Line, msg: fmt.Sprintf(format, a...)}
}

// parseValue reads n as a value of type typ: an alias "{group.token}" or a
// literal. unit, when set, lets a bare number stand for a dimension or
// duration in that unit (the density shorthand).
func parseValue(n *ojson.Node, typ, unit, path string) (*Value, *shapeErr) {
	if n.Kind == ojson.String {
		if m := aliasRE.FindStringSubmatch(n.Str); m != nil {
			segs := strings.Split(m[1], ".")
			if slices.Contains(segs, "") {
				return nil, bad(n, path, "alias %q has an empty path segment", n.Str)
			}
			return &Value{Alias: strings.Join(segs, "-"), Line: n.Line}, nil
		}
	}
	var lit any
	var err *shapeErr
	switch typ {
	case "color":
		lit, err = parseColor(n, path)
	case "dimension":
		if n.Kind == ojson.Number && unit != "" {
			lit = Dim{n.Num, unit}
			break
		}
		var d Duration
		d, err = parseUnit(n, path, "dimension", "px", "rem")
		lit = Dim(d)
	case "duration":
		if n.Kind == ojson.Number && unit != "" {
			lit = Duration{n.Num, unit}
			break
		}
		lit, err = parseUnit(n, path, "duration", "ms", "s")
	case "number":
		if n.Kind != ojson.Number {
			return nil, bad(n, path, "a number token needs a number")
		}
		lit = n.Num
	case "fontFamily":
		lit, err = parseFamily(n, path)
	case "fontWeight":
		lit, err = parseWeight(n, path)
	case "cubicBezier":
		lit, err = parseBezier(n, path)
	case "shadow":
		lit, err = parseShadow(n, path)
	}
	if err != nil {
		return nil, err
	}
	return &Value{Lit: lit, Line: n.Line}, nil
}

func number(n *ojson.Node) (float64, bool) {
	if n == nil || n.Kind != ojson.Number || math.IsNaN(n.Num) || math.IsInf(n.Num, 0) {
		return 0, false
	}
	return n.Num, true
}

// keys reports the first key of object n that is not in allowed.
func keys(n *ojson.Node, path string, allowed ...string) *shapeErr {
	for _, k := range n.Keys {
		if !slices.Contains(allowed, k) {
			return bad(n.Fields[k], path+"."+k, "unknown key %q", k)
		}
	}
	return nil
}

func parseColor(n *ojson.Node, path string) (color.OKLCH, *shapeErr) {
	if n.Kind != ojson.Object {
		if n.Kind == ojson.String {
			return color.OKLCH{}, bad(n, path, `a color must be an alias "{group.token}" or an oklch color object`)
		}
		return color.OKLCH{}, bad(n, path, "a color must be an oklch color object")
	}
	if e := keys(n, path, "colorSpace", "components", "alpha", "hex"); e != nil {
		return color.OKLCH{}, e
	}
	if cs, ok := n.Fields["colorSpace"]; !ok {
		return color.OKLCH{}, bad(n, path, `colorSpace must be "oklch"`)
	} else if cs.Kind != ojson.String || cs.Str != "oklch" {
		return color.OKLCH{}, bad(cs, path+".colorSpace", `colorSpace must be "oklch"`)
	}
	comps, ok := n.Fields["components"]
	if !ok || comps.Kind != ojson.Array || len(comps.Items) != 3 {
		at := n
		if ok {
			at = comps
		}
		return color.OKLCH{}, bad(at, path+".components", "components must be [L, C, H]")
	}
	var lch [3]float64
	for i, it := range comps.Items {
		if lch[i], ok = number(it); !ok {
			return color.OKLCH{}, bad(it, path+".components", "components must be [L, C, H] numbers")
		}
	}
	c := color.OKLCH{L: lch[0], C: lch[1], H: lch[2], Alpha: 1}
	switch {
	case c.L < 0 || c.L > 1:
		return c, bad(comps.Items[0], path+".components", "lightness must be between 0 and 1")
	case c.C < 0:
		return c, bad(comps.Items[1], path+".components", "chroma must not be negative")
	case c.H < 0 || c.H > 360:
		return c, bad(comps.Items[2], path+".components", "hue must be between 0 and 360")
	}
	if a, has := n.Fields["alpha"]; has {
		v, ok := number(a)
		if !ok || v < 0 || v > 1 {
			return c, bad(a, path+".alpha", "alpha must be a number between 0 and 1")
		}
		c.Alpha = v
	}
	if h, has := n.Fields["hex"]; has && h.Kind != ojson.String {
		return c, bad(h, path+".hex", "hex must be a string")
	}
	return c, nil
}

// parseUnit reads a {value, unit} object; the result is converted to Dim by
// the dimension caller.
func parseUnit(n *ojson.Node, path, what string, units ...string) (Duration, *shapeErr) {
	if n.Kind != ojson.Object {
		return Duration{}, bad(n, path, "a %s must be {value, unit}", what)
	}
	if e := keys(n, path, "value", "unit"); e != nil {
		return Duration{}, e
	}
	v, ok := number(n.Fields["value"])
	if !ok {
		return Duration{}, bad(n, path+".value", "a %s needs a numeric value", what)
	}
	u := n.Fields["unit"]
	if u == nil || u.Kind != ojson.String || !slices.Contains(units, u.Str) {
		at := n
		if u != nil {
			at = u
		}
		return Duration{}, bad(at, path+".unit", "%s unit must be %s", what, strings.Join(quoteAll(units), " or "))
	}
	return Duration{v, u.Str}, nil
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

func parseFamily(n *ojson.Node, path string) ([]string, *shapeErr) {
	switch n.Kind {
	case ojson.String:
		if n.Str == "" {
			return nil, bad(n, path, "fontFamily must not be empty")
		}
		return []string{n.Str}, nil
	case ojson.Array:
		if len(n.Items) == 0 {
			return nil, bad(n, path, "fontFamily must not be empty")
		}
		out := make([]string, 0, len(n.Items))
		for _, it := range n.Items {
			if it.Kind != ojson.String || it.Str == "" {
				return nil, bad(it, path, "fontFamily must be a name or a list of names")
			}
			out = append(out, it.Str)
		}
		return out, nil
	}
	return nil, bad(n, path, "fontFamily must be a name or a list of names")
}

func parseWeight(n *ojson.Node, path string) (float64, *shapeErr) {
	if n.Kind == ojson.String {
		if w, ok := fontWeights[n.Str]; ok {
			return w, nil
		}
		return 0, bad(n, path, "unknown fontWeight keyword %q", n.Str)
	}
	w, ok := number(n)
	if !ok || w < 1 || w > 1000 {
		return 0, bad(n, path, "fontWeight must be a number from 1 to 1000 or a weight keyword")
	}
	return w, nil
}

func parseBezier(n *ojson.Node, path string) ([4]float64, *shapeErr) {
	var out [4]float64
	if n.Kind != ojson.Array || len(n.Items) != 4 {
		return out, bad(n, path, "cubicBezier must be [x1, y1, x2, y2]")
	}
	for i, it := range n.Items {
		v, ok := number(it)
		if !ok {
			return out, bad(it, path, "cubicBezier must be [x1, y1, x2, y2] numbers")
		}
		if (i == 0 || i == 2) && (v < 0 || v > 1) {
			return out, bad(it, path, "cubicBezier x values must be between 0 and 1")
		}
		out[i] = v
	}
	return out, nil
}

func parseShadow(n *ojson.Node, path string) ([]Shadow, *shapeErr) {
	layers := []*ojson.Node{n}
	if n.Kind == ojson.Array {
		if len(n.Items) == 0 {
			return nil, bad(n, path, "shadow must have at least one layer")
		}
		layers = n.Items
	}
	var out []Shadow
	for _, l := range layers {
		if l.Kind != ojson.Object {
			return nil, bad(l, path, "a shadow layer must be an object")
		}
		if e := keys(l, path, "color", "offsetX", "offsetY", "blur", "spread", "inset"); e != nil {
			return nil, e
		}
		var s Shadow
		for _, k := range []string{"color", "offsetX", "offsetY", "blur", "spread"} {
			f, ok := l.Fields[k]
			if !ok {
				return nil, bad(l, path, "a shadow layer needs %s", k)
			}
			if f.Kind == ojson.String && aliasRE.MatchString(f.Str) {
				return nil, bad(f, path+"."+k, "aliases inside a shadow are not supported; alias the whole shadow")
			}
			var err *shapeErr
			switch k {
			case "color":
				s.Color, err = parseColor(f, path+"."+k)
			default:
				var d Duration
				d, err = parseUnit(f, path+"."+k, "dimension", "px", "rem")
				dim := Dim(d)
				switch k {
				case "offsetX":
					s.OffsetX = dim
				case "offsetY":
					s.OffsetY = dim
				case "blur":
					s.Blur = dim
				case "spread":
					s.Spread = dim
				}
			}
			if err != nil {
				return nil, err
			}
		}
		if in, ok := l.Fields["inset"]; ok {
			if in.Kind != ojson.Bool {
				return nil, bad(in, path+".inset", "inset must be true or false")
			}
			s.Inset = in.Bool
		}
		out = append(out, s)
	}
	return out, nil
}
