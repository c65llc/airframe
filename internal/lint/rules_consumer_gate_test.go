// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
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

type gateDoc struct {
	Checks []struct {
		ID      string `json:"id"`
		Scanned int    `json:"scanned"`
	} `json:"checks"`
	Findings []struct {
		Rule string `json:"rule"`
	} `json:"findings"`
	NotApplicable []struct {
		ID string `json:"id"`
	} `json:"notApplicable"`
}

// Review Focus 5: a glob typo fails lint, naming the platform and the
// glob, and the rule that reports it is a check that ran, never n/a.
func TestEmptyScanFails(t *testing.T) {
	fsys := project.MemFS{
		"airframe.yaml":               "version: 1\ncontent: design-system\nplatforms:\n  web:\n    emit: [css]\n    out: {css: web/generated/tokens.css}\n    consumers: [\"web/**/*.tssx\"]\n",
		"design-system/a.tokens.json": `{ "gap": { "$type": "number", "$value": 4, "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } } }`,
		"web/src/a.ts":                "export const a = 1\n",
	}
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	res := lint.Run(p, nil, lint.Options{})
	var got []diag.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Rule == "reference-undeclared" {
			got = append(got, d)
		}
	}
	require.Len(t, got, 1)
	assert.Equal(t, diag.Error, got[0].Severity)
	assert.Contains(t, got[0].Message, "platform web")
	assert.Contains(t, got[0].Message, `"web/**/*.tssx"`)
	assert.Equal(t, "airframe.yaml", got[0].File)
	assert.Equal(t, "platforms.web.consumers", got[0].Path)
	assert.Positive(t, res.Errors)

	var text bytes.Buffer
	diag.WriteText(&text, res.Diagnostics, res.Tallies)
	assert.Contains(t, text.String(), "\nreference-undeclared  1 references and globs\n")
	assert.NotContains(t, text.String(), "reference-undeclared  n/a")

	var buf bytes.Buffer
	require.NoError(t, gate.Write(&buf, res, selftest.Result{OK: true}, len(p.Sources)))
	var doc gateDoc
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	checks := map[string]int{}
	for _, c := range doc.Checks {
		checks[c.ID] = c.Scanned
	}
	assert.Equal(t, 1, checks["reference-undeclared"], "the one glob was examined")
	for _, n := range doc.NotApplicable {
		assert.NotEqual(t, "reference-undeclared", n.ID)
	}
	assert.True(t, strings.Contains(buf.String(), `"rule": "reference-undeclared"`))
}
