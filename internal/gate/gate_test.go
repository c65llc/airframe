// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package gate

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/selftest"
)

const cfg = `version: 1
modes:
  appearance: {values: [light, dark]}
`

// lintFS formats every file in fsys (so not-formatted stays quiet), then
// loads and lints it, failing on load diagnostics.
func lintFS(t *testing.T, fsys project.MemFS) (lint.Result, int) {
	t.Helper()
	for name, data := range fsys {
		out, err := format.File(name, []byte(data))
		require.NoError(t, err, name)
		fsys[name] = string(out)
	}
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	return lint.Run(p, nil, lint.Options{}), len(p.Sources)
}

type doc struct {
	Gate   string `json:"gate"`
	Checks []struct {
		ID      string `json:"id"`
		Scanned int    `json:"scanned"`
	} `json:"checks"`
	Selftest struct {
		Flagged, Ignored int
		OK               bool
	} `json:"selftest"`
	Scanned struct {
		Files, Items int
	} `json:"scanned"`
	Findings []struct {
		File, Message, Rule string
		Line                int
	} `json:"findings"`
	Exemptions []struct {
		File, Marker, Reason string
		Line                 int
	} `json:"exemptions"`
	NotApplicable []struct {
		ID, Reason string
	} `json:"notApplicable"`
}

func write(t *testing.T, r lint.Result, st selftest.Result, files int) (string, doc) {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, Write(&b, r, st, files))
	var d doc
	require.NoError(t, json.Unmarshal(b.Bytes(), &d))
	return b.String(), d
}

func TestGateShape(t *testing.T) {
	res, files := lintFS(t, project.MemFS{
		"airframe.yaml": cfg,
		"design-system/a.tokens.json": `{
  "gap": { "$type": "number", "$value": 4, "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } }
}
`,
	})
	raw, d := write(t, res, selftest.Run(), files)

	assert.Equal(t, "airframe", d.Gate)
	assert.True(t, d.Selftest.OK)
	assert.Equal(t, len(lint.AllIDs()), d.Selftest.Flagged)
	assert.Equal(t, len(lint.AllIDs()), d.Selftest.Ignored)
	require.NotEmpty(t, d.Checks)
	items := 0
	for _, c := range d.Checks {
		assert.Positive(t, c.Scanned, c.ID)
		items += c.Scanned
	}
	assert.Equal(t, 2, d.Scanned.Files, "airframe.yaml and the token file")
	assert.Equal(t, items, d.Scanned.Items)

	var na []string
	for _, n := range d.NotApplicable {
		na = append(na, n.ID+": "+n.Reason)
	}
	assert.Contains(t, na, "contrast-below: no contrast invariants declared", "a declared-nothing contrast")
	assert.Contains(t, na, "literal-in-semantic: no tokens in scope", "an empty scan never reads as a check")

	assert.Empty(t, d.Findings)
	assert.Contains(t, raw, `"findings": []`, "empty lists are [], never null")
	assert.Contains(t, raw, `"exemptions": []`)
	assert.True(t, strings.HasSuffix(raw, "}\n"))
}

func TestGateFindingsAndExemptions(t *testing.T) {
	res, files := lintFS(t, project.MemFS{
		"airframe.yaml": cfg,
		"design-system/a.tokens.json": `{
  "bare": { "$type": "number", "$value": 1 },
  "old": { "$type": "number", "$value": 1,
    "$extensions": { "com.c65llc.airframe": { "exempt": { "token-unclassified": "legacy import, #12" } } } }
}`,
	})
	raw, d := write(t, res, selftest.Result{Flagged: 1, Failures: []string{"flag: x did not fire"}}, files)

	assert.False(t, d.Selftest.OK)
	assert.Contains(t, raw, `"failures": [`)
	require.NotEmpty(t, d.Findings)
	var rules []string
	for _, f := range d.Findings {
		rules = append(rules, f.Rule)
		assert.Equal(t, "design-system/a.tokens.json", f.File)
		assert.NotEmpty(t, f.Message)
	}
	assert.Contains(t, rules, "token-unclassified")
	require.Len(t, d.Exemptions, 1)
	assert.Equal(t, "design-system/a.tokens.json", d.Exemptions[0].File)
	assert.Positive(t, d.Exemptions[0].Line)
	assert.Equal(t, "exempt", d.Exemptions[0].Marker)
	assert.Equal(t, "legacy import, #12", d.Exemptions[0].Reason)
}

func TestGateNilListsAndStable(t *testing.T) {
	var a, b bytes.Buffer
	require.NoError(t, Write(&a, lint.Result{}, selftest.Result{}, 0))
	require.NoError(t, Write(&b, lint.Result{}, selftest.Result{}, 0))
	assert.Equal(t, a.String(), b.String())
	for _, k := range []string{"checks", "findings", "exemptions", "notApplicable"} {
		assert.Contains(t, a.String(), `"`+k+`": []`, k)
	}
	assert.NotContains(t, a.String(), "null")
	assert.NotContains(t, a.String(), "failures", "failures appear only when there are some")
}

// TestEveryRuleInExactlyOneGateList: each rule is either a check (it
// scanned something) or not applicable, never both and never neither, on
// the self-test's clean fixture, a one-token project and every flag fixture
// that loads.
func TestEveryRuleInExactlyOneGateList(t *testing.T) {
	projects := map[string]fs.FS{
		"one-token": project.MemFS{
			"airframe.yaml":               cfg,
			"design-system/a.tokens.json": `{ "gap": { "$type": "number", "$value": 4 } }`,
		},
		"clean": os.DirFS("../selftest/fixtures/clean"),
	}
	flags, err := os.ReadDir("../selftest/fixtures/flag")
	require.NoError(t, err)
	for _, e := range flags {
		projects["flag/"+e.Name()] = os.DirFS("../selftest/fixtures/flag/" + e.Name())
	}
	linted := 0
	for name, fsys := range projects {
		p, ds, err := project.Load(fsys)
		require.NoError(t, err, name)
		if p == nil {
			continue // loading stopped: no rule ran
		}
		linted++
		_, d := write(t, lint.Run(p, ds, lint.Options{}), selftest.Result{}, len(p.Sources))
		seen := map[string]int{}
		for _, c := range d.Checks {
			seen[c.ID]++
		}
		for _, n := range d.NotApplicable {
			seen[n.ID]++
			assert.NotEmpty(t, n.Reason, "%s: %s", name, n.ID)
		}
		for _, r := range lint.Rules() {
			assert.Equal(t, 1, seen[r.ID], "%s: %s must be in exactly one of checks / notApplicable", name, r.ID)
		}
		assert.Len(t, seen, len(lint.Rules()), name)
	}
	assert.Greater(t, linted, 20, "most flag fixtures load")
}
