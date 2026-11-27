// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
)

func TestJSONGolden(t *testing.T) {
	out := buildMinimal(t)
	assert.Equal(t, string(golden(t, minimalJSON)), string(out[minimalJSON]))
}

func TestJSONShape(t *testing.T) {
	out := buildMinimal(t)
	var doc struct {
		Version int `json:"version"`
		Modes   map[string]struct {
			Values []string `json:"values"`
			Base   string   `json:"base"`
		} `json:"modes"`
		Tokens map[string]struct {
			Type      string                     `json:"type"`
			Tier      string                     `json:"tier"`
			Component string                     `json:"component"`
			Value     json.RawMessage            `json:"value"`
			Modes     map[string]json.RawMessage `json:"modes"`
		} `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal(out[minimalJSON], &doc))
	assert.Equal(t, 1, doc.Version)
	assert.Equal(t, []string{"compact", "standard", "relaxed"}, doc.Modes["density"].Values)
	assert.Equal(t, "standard", doc.Modes["density"].Base)
	assert.Equal(t, "light", doc.Modes["appearance"].Base)

	accentInk := doc.Tokens["accent-ink"]
	assert.Equal(t, "color", accentInk.Type)
	assert.Equal(t, "semantic", accentInk.Tier)
	assert.JSONEq(t, `{"colorSpace":"oklch","components":[0.63,0.13,75]}`, string(accentInk.Value), "aliases resolve")
	assert.JSONEq(t, `{"colorSpace":"oklch","components":[0.78,0.12,75]}`, string(accentInk.Modes["appearance:dark"]),
		"a mode reached through an alias is listed on the aliasing token")

	btn := doc.Tokens["button-target"]
	assert.Equal(t, "button", btn.Component)
	assert.JSONEq(t, `{"value":56,"unit":"px"}`, string(btn.Modes["density:relaxed"]))

	assert.JSONEq(t, `["IBM Plex Sans","system-ui"]`, string(doc.Tokens["text-body"].Value))
	assert.JSONEq(t, `[0.2,0,0,1]`, string(doc.Tokens["motion-ease"].Value))
	assert.JSONEq(t, `700`, string(doc.Tokens["text-strong"].Value))
	assert.JSONEq(t, `{"value":120,"unit":"ms"}`, string(doc.Tokens["motion-fast"].Value))
	assert.JSONEq(t, `{"colorSpace":"oklch","components":[0.63,0.13,75],"alpha":0.5}`, string(doc.Tokens["accent-glow"].Value))
	assert.JSONEq(t, `[{"color":{"colorSpace":"oklch","components":[0.2,0.012,250],"alpha":0.2},
	  "offsetX":{"value":0,"unit":"px"},"offsetY":{"value":1,"unit":"px"},"blur":{"value":2,"unit":"px"},"spread":{"value":0,"unit":"px"}}]`,
		string(doc.Tokens["elevation-low"].Value))
	assert.Empty(t, doc.Tokens["gap-sm"].Modes)
}

// TestJSONModeClosure: a token whose own key and whose alias target's key
// meet in a context lists their union, so a reader applying the
// most-pairs rule to the JSON alone never sees a tie.
func TestJSONModeClosure(t *testing.T) {
	p := big576With(t, `{
  "base": { "$type": "number", "$value": 0, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "modes": { "appearance:dark": 1 } } } },
  "x": { "$type": "number", "$value": "{base}", "$extensions": { "com.c65llc.airframe": { "modes": { "density:compact": 5 } } } } }`)
	out, err := JSON(p, config.Platform{Name: "json", Emit: []string{"json"}})
	require.NoError(t, err)
	var doc struct {
		Tokens map[string]struct {
			Modes map[string]json.RawMessage `json:"modes"`
		} `json:"tokens"`
	}
	for _, b := range out {
		require.NoError(t, json.Unmarshal(b, &doc))
	}
	m := doc.Tokens["x"].Modes
	assert.JSONEq(t, `1`, string(m["appearance:dark"]))
	assert.JSONEq(t, `5`, string(m["density:compact"]))
	assert.JSONEq(t, `5`, string(m["appearance:dark+density:compact"]))
	assert.Len(t, m, 3)
}

// TestJSONModesDeferredAxis: a deferred axis is listed with no values, and
// with its base when it declares one.
func TestJSONModesDeferredAxis(t *testing.T) {
	fsys := memFS(t, minimalDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "platforms:",
		"  swatch: {values: from brands/s.tokens.json, base: moss, web: {attr: data-s}}\n  tint: {values: from brands/t.tokens.json, web: {attr: data-t}}\nplatforms:", 1)
	p := loadFS(t, fsys)
	out, err := JSON(p, p.Config.Platforms[1])
	require.NoError(t, err)
	var doc struct {
		Modes map[string]json.RawMessage `json:"modes"`
	}
	require.NoError(t, json.Unmarshal(out[minimalJSON], &doc))
	assert.JSONEq(t, `{"values": [], "base": "moss"}`, string(doc.Modes["swatch"]))
	assert.JSONEq(t, `{"values": []}`, string(doc.Modes["tint"]))
}
