// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func variants(t *testing.T, dir, platform, format string) map[string]Variant {
	t.Helper()
	p := loadFS(t, os.DirFS(dir))
	vs, err := ReferenceVariants(p, platformNamed(t, p, platform), format)
	require.NoError(t, err)
	return vs
}

func TestReferenceVariants(t *testing.T) {
	xaml := variants(t, windowsDir, "windows", "xaml")
	assert.Equal(t, Variant{"gap", ""}, xaml["DesignGap"])
	assert.Equal(t, Variant{"gap", "density=compact"}, xaml["DesignGapCompact"])
	assert.Equal(t, Variant{"card-shadow", "layer1"}, xaml["DesignCardShadowLayer1"], "a shadow layer brush")
	assert.Equal(t, Variant{"card-shadow", "layer1.offsetX"}, xaml["DesignCardShadowLayer1OffsetX"], "a shadow part key")
	assert.Equal(t, Variant{"", "theme:Light"}, xaml["Light"])

	kt := variants(t, androidDir, "android", "kotlin")
	assert.Equal(t, Variant{"surface", ""}, kt["surface"], "a member wins over the slot of the same name")
	assert.Equal(t, Variant{"surface", "slot:density=compact"}, kt["surfaceCompact"])
	assert.Equal(t, Variant{"card-shadow", "slot:layer1"}, kt["cardShadowLayer1"])
	assert.Equal(t, Variant{"gap", ""}, kt["gap"])
	assert.Equal(t, Variant{"", "values:light"}, kt["light"])
	assert.Equal(t, Variant{"", "scheme:light"}, kt["lightScheme"])
	assert.Equal(t, Variant{"", "symbol:Companion"}, kt["Companion"])

	sw := variants(t, appleDir, "apple", "swift")
	assert.Equal(t, Variant{"default", ""}, sw["`default`"])
	assert.Equal(t, Variant{"default", ""}, sw["default"])
	assert.Equal(t, Variant{"", "symbol:DesignTokens"}, sw["DesignTokens"])

	css := variants(t, minimalDir, "web", "css")
	assert.Equal(t, Variant{"accent-ink", ""}, css["--accent-ink"])

	// VariantNames inverts the map: every variant's name maps back to it, and
	// a variant spelled two ways takes its escaped spelling.
	for _, vs := range []map[string]Variant{xaml, kt, sw, css} {
		names := VariantNames(vs)
		for v, n := range names {
			assert.Equal(t, v, vs[n], "VariantNames(%v) = %s", v, n)
		}
		for n, v := range vs {
			assert.Contains(t, names, v, "%s's variant has no name", n)
		}
	}
	assert.Equal(t, "`default`", VariantNames(sw)[Variant{"default", ""}])
	assert.Equal(t, "DesignGapCompact", VariantNames(xaml)[Variant{"gap", "density=compact"}])
}

// ReferenceNames is ReferenceVariants' tokens, name for name.
func TestReferenceNamesUnchanged(t *testing.T) {
	for _, g := range refnamesGolden {
		t.Run(g.name, func(t *testing.T) {
			fsys := os.DirFS(g.dir)
			if g.fsys != nil {
				fsys = g.fsys(t)
			}
			p := loadFS(t, fsys)
			pl := platformNamed(t, p, g.platform)
			names, err := ReferenceNames(p, pl, g.format)
			require.NoError(t, err)
			vs, err := ReferenceVariants(p, pl, g.format)
			require.NoError(t, err)
			require.Len(t, names, len(vs))
			for n, v := range vs {
				assert.Equal(t, v.Token, names[n], n)
			}
		})
	}
}
