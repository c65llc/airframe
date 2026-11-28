// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/gate"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/selftest"
)

// TestConsumerExemptionInGate: an airframe-exempt marker with a reason
// suppresses its finding and is listed in the gate JSON's exemptions, so
// a gate reader sees what was waived and why.
func TestConsumerExemptionInGate(t *testing.T) {
	fsys := project.MemFS{
		"airframe.yaml": "version: 1\ncontent: design-system\nplatforms:\n  web:\n    emit: [css]\n    out: {css: web/generated/tokens.css}\n    consumers: [\"web/**/*.css\"]\n",
		"design-system/a.tokens.json": `{ "blue": { "$type": "color", "$value": { "colorSpace": "oklch", "components": [0.55, 0.2, 260] }, ` +
			`"$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`,
		"web/a.css": ".a { color: var(--blue); } /* airframe-exempt primitive-read-by-consumer: brand banner */\n",
	}
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	res := lint.Run(p, nil, lint.Options{})
	require.NoError(t, res.Err)
	var buf bytes.Buffer
	require.NoError(t, gate.Write(&buf, res, selftest.Result{OK: true}, len(p.Sources)))
	var doc struct {
		Checks []struct {
			ID      string `json:"id"`
			Scanned int    `json:"scanned"`
		} `json:"checks"`
		Findings []struct {
			Rule string `json:"rule"`
		} `json:"findings"`
		Exemptions []map[string]any `json:"exemptions"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	assert.Equal(t, []map[string]any{{
		"file": "web/a.css", "line": float64(1), "marker": "airframe-exempt", "reason": "brand banner",
	}}, doc.Exemptions)
	for _, f := range doc.Findings {
		assert.NotEqual(t, "primitive-read-by-consumer", f.Rule)
	}
	scanned := map[string]int{}
	for _, c := range doc.Checks {
		scanned[c.ID] = c.Scanned
	}
	assert.Equal(t, 1, scanned["primitive-read-by-consumer"], "the check ran on one reference")
}
