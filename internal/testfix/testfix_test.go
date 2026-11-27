// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package testfix

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
)

func TestBig576Shape(t *testing.T) {
	p, ds, err := project.Load(Big576())
	require.NoError(t, err)
	require.Empty(t, ds)
	require.NotNil(t, p)
	assert.Empty(t, p.Tokens.Check())

	var names []string
	for _, a := range p.Config.Axes {
		names = append(names, a.Name)
	}
	assert.Len(t, p.Config.Contexts(names), 576)
	assert.Len(t, p.Tokens.Order, 200)
	assert.Len(t, p.Invariants, 4)

	tiers := map[string]int{}
	keys := map[config.ModeKey]bool{}
	for _, tok := range p.Tokens.Tokens {
		tiers[tok.Tier]++
		for k := range tok.Modes {
			keys[k] = true
		}
	}
	assert.Equal(t, map[string]int{"primitive": 86, "semantic": 42, "component": 72}, tiers)
	assert.Equal(t, []config.ModeKey{
		"app:tasks", "app:tasks+appearance:dark", "appearance:dark", "channel:next",
		"density:compact", "density:relaxed",
	}, slices.Sorted(maps.Keys(keys)))
}
