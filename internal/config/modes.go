// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package config

import (
	"fmt"
	"slices"
	"strings"
)

// ModeKey is a canonical mode selector such as "app:slides+appearance:dark":
// axis:value pairs sorted by axis name and joined with "+". The empty key
// selects nothing in particular.
type ModeKey string

// Context assigns one value to each axis.
type Context map[string]string

func (c *Config) axis(name string) (Axis, bool) {
	for _, a := range c.Axes {
		if a.Name == name {
			return a, true
		}
	}
	return Axis{}, false
}

// ParseModeKey parses s into canonical order, rejecting unknown axes,
// unknown values, deferred axes (no values loaded) and repeated axes.
func (c *Config) ParseModeKey(s string) (ModeKey, error) {
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	seen := map[string]string{}
	for _, part := range strings.Split(s, "+") {
		name, val, ok := strings.Cut(strings.TrimSpace(part), ":")
		name, val = strings.TrimSpace(name), strings.TrimSpace(val)
		if !ok || name == "" || val == "" {
			return "", fmt.Errorf("mode %q is not axis:value", part)
		}
		ax, ok := c.axis(name)
		if !ok {
			return "", fmt.Errorf("unknown axis %q", name)
		}
		if ax.Deferred() {
			return "", fmt.Errorf("axis %s has no values loaded", name)
		}
		if !slices.Contains(ax.Values, val) {
			return "", fmt.Errorf("unknown value %q for axis %s", val, name)
		}
		if prev, dup := seen[name]; dup {
			return "", fmt.Errorf("axis %s given twice (%s, %s)", name, prev, val)
		}
		seen[name] = val
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	slices.Sort(names)
	pairs := make([]string, len(names))
	for i, n := range names {
		pairs[i] = n + ":" + seen[n]
	}
	return ModeKey(strings.Join(pairs, "+")), nil
}

// CheckAxes returns an error naming the first of names that is not a
// configured axis or whose values are not loaded yet.
func (c *Config) CheckAxes(names []string) error {
	for _, n := range names {
		ax, ok := c.axis(n)
		if !ok {
			return fmt.Errorf("unknown axis %q", n)
		}
		if ax.Deferred() {
			return fmt.Errorf("axis %s has no values loaded", n)
		}
	}
	return nil
}

// Contexts returns the product of the values of the named axes, in config
// order with the first-named-in-config axis varying slowest. Every other
// axis sits at its first value. Deferred axes and unknown names are left
// out, so callers must CheckAxes first or a typo shrinks the product silently.
func (c *Config) Contexts(axes []string) []Context {
	out := []Context{{}}
	for _, a := range c.Axes {
		if a.Deferred() || len(a.Values) == 0 {
			continue
		}
		vals := a.Values[:1]
		if slices.Contains(axes, a.Name) {
			vals = a.Values
		}
		next := make([]Context, 0, len(out)*len(vals))
		for _, ctx := range out {
			for _, v := range vals {
				n := make(Context, len(ctx)+1)
				for k, x := range ctx {
					n[k] = x
				}
				n[a.Name] = v
				next = append(next, n)
			}
		}
		out = next
	}
	return out
}

func (k ModeKey) pairs() []string {
	if k == "" {
		return nil
	}
	return strings.Split(string(k), "+")
}

// Matches reports whether every axis:value pair of k holds in ctx.
func (k ModeKey) Matches(ctx Context) bool {
	for _, p := range k.pairs() {
		name, val, _ := strings.Cut(p, ":")
		if ctx[name] != val {
			return false
		}
	}
	return true
}

// Pairs is the number of axis:value pairs in k, its specificity.
func (k ModeKey) Pairs() int { return len(k.pairs()) }

// IsBase reports whether v is the axis's base value: its only web selector
// is exactly ":root", so CSS cannot tell it from "axis unset" and a mode
// key must not name it.
func (a Axis) IsBase(v string) bool {
	sels := a.Web.Selectors[v]
	return len(sels) == 1 && strings.TrimSpace(sels[0]) == ":root"
}

// BasePair is the first pair of k that names its axis's base value, if any.
func (c *Config) BasePair(k ModeKey) (axis, value string, ok bool) {
	for _, p := range k.pairs() {
		name, val, _ := strings.Cut(p, ":")
		if ax, found := c.axis(name); found && ax.IsBase(val) {
			return name, val, true
		}
	}
	return "", "", false
}
