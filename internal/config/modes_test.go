// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func specConfig(t *testing.T) *Config { return mustLoad(t, specExample) }

func TestParseModeKeyCanonical(t *testing.T) {
	c := specConfig(t)
	k, err := c.ParseModeKey("appearance:dark+app:slides")
	require.NoError(t, err)
	assert.Equal(t, ModeKey("app:slides+appearance:dark"), k)
	assert.Equal(t, 2, k.Pairs())

	k, err = c.ParseModeKey("")
	require.NoError(t, err)
	assert.Equal(t, 0, k.Pairs())
	assert.True(t, k.Matches(Context{"app": "wiki"}))
}

func TestParseModeKeyUnknown(t *testing.T) {
	c := specConfig(t)
	_, err := c.ParseModeKey("appearance:sepia")
	assert.EqualError(t, err, `unknown value "sepia" for axis appearance`)
	_, err = c.ParseModeKey("flavor:x")
	assert.EqualError(t, err, `unknown axis "flavor"`)
	_, err = c.ParseModeKey("appearance")
	assert.Error(t, err)
	_, err = c.ParseModeKey("appearance:dark+appearance:light")
	assert.Error(t, err)
}

func TestContextsProduct(t *testing.T) {
	c := specConfig(t)
	got := c.Contexts([]string{"density", "appearance"}) // order of the argument is irrelevant
	require.Len(t, got, 6)
	var order []string
	for _, ctx := range got {
		assert.Equal(t, "drafts", ctx["app"])
		assert.Equal(t, "stable", ctx["channel"])
		order = append(order, ctx["appearance"]+"/"+ctx["density"])
	}
	assert.Equal(t, []string{
		"light/compact", "light/standard", "light/relaxed",
		"dark/compact", "dark/standard", "dark/relaxed",
	}, order)
	assert.Len(t, c.Contexts(nil), 1)
}

func TestDeferredAxisExcluded(t *testing.T) {
	c := specConfig(t)
	for _, ctx := range c.Contexts([]string{"appearance", "swatch"}) {
		assert.NotContains(t, ctx, "swatch")
	}
	_, err := c.ParseModeKey("swatch:moss")
	assert.EqualError(t, err, "axis swatch has no values loaded")
}

func TestMatches(t *testing.T) {
	c := specConfig(t)
	k, err := c.ParseModeKey("appearance:dark")
	require.NoError(t, err)
	assert.True(t, k.Matches(Context{"appearance": "dark", "app": "wiki"}))
	assert.False(t, k.Matches(Context{"appearance": "light"}))
}

func TestCheckAxesOK(t *testing.T) {
	assert.NoError(t, specConfig(t).CheckAxes([]string{"appearance", "density"}))
	assert.NoError(t, specConfig(t).CheckAxes(nil))
}

func TestCheckAxesUnknown(t *testing.T) {
	assert.EqualError(t, specConfig(t).CheckAxes([]string{"appearance", "apperance"}), `unknown axis "apperance"`)
}

func TestCheckAxesDeferred(t *testing.T) {
	assert.EqualError(t, specConfig(t).CheckAxes([]string{"swatch"}), "axis swatch has no values loaded")
}
