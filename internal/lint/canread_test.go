// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

func TestCanRead(t *testing.T) {
	fsys := consumerFS(consumerConfig, map[string]string{
		"design-system/components/chip.yaml": "id: chip\ntokens: [chip-bg]\nshared: true\n",
		"design-system/components/pill.yaml": "id: pill\ntokens: [pill-bg]\nowners: 7\n", // does not load
	})
	p, _, err := project.Load(fsys)
	require.NoError(t, err)
	require.NotNil(t, p)
	for _, tc := range []struct {
		file, token string
		ok          bool
		why         string
	}{
		{"web/src/page.ts", "accent", true, "semantic: consumers may read"},
		{"docs/notes.md", "accent", true, "semantic: consumers may read (not a consumer file of any platform)"},
		{"web/src/page.ts", "blue-500", false, "primitive: consumers may not read"},
		{"web/src/button/b.ts", "button-bg", true, "component button: owners include web/src/button/**"},
		{"web/src/page.ts", "button-bg", false, "component button: owners (web/src/button/**) do not match web/src/page.ts"},
		{"web/src/page.ts", "chip-bg", true, "component chip: shared"},
		// Conservative where lint is not: lint leaves a component whose
		// file failed to load to schema-invalid, but CanRead refuses.
		{"web/src/page.ts", "pill-bg", false, "component pill: no component file declares its owners"},
	} {
		got, why := CanRead(p, tc.file, p.Tokens.Tokens[tc.token])
		assert.Equal(t, tc.ok, got, "%s reads %s", tc.file, tc.token)
		assert.Equal(t, tc.why, why, "%s reads %s", tc.file, tc.token)
	}
	// A component-tier token that names no component is refused too; lint
	// has no owner to check it against and says nothing.
	ok, why := CanRead(p, "web/src/page.ts", &tokens.Token{Name: "loose", Tier: tierComponent})
	assert.False(t, ok)
	assert.Equal(t, "component-tier token with no component: no owners to read it", why)
}
