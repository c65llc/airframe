// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllIDsSortedAndComplete(t *testing.T) {
	ids := AllIDs()
	assert.True(t, slices.IsSorted(ids), ids)
	assert.Len(t, slices.Compact(slices.Clone(ids)), len(ids), "no duplicates")
	for _, id := range ruleIDs() {
		assert.Contains(t, ids, id, "every registered rule")
	}
	for _, id := range []string{
		"alias-cycle", "alias-type-mismatch", "alias-unresolved", "config-missing", "emit-failed",
		"json-syntax", "mode-ambiguous", "mode-base-value", "mode-override-conflict", "mode-unknown",
		"no-tokens", "schema-invalid", "token-name-invalid", "type-unsupported", "yaml-syntax",
	} {
		assert.Contains(t, ids, id, "load/build-time ID")
	}
}

// ruleLiteral matches the ways the code names a rule ID when it builds a
// diagnostic: a Rule field, a rule-ID constant, a registered rule's ID, a
// docs link, or the first argument of a diagnostic constructor.
var ruleLiteral = regexp.MustCompile(`(?:Rule:\s*|\.Rule\s*=\s*|ID:\s*|const (?:id|RuleID)\s*=\s*|DocsURL\(|\b(?:report|finding|diagnostic)\()"([a-z][a-z0-9-]*)"`)

// TestAllIDsCoversEveryEmittedID scans the non-test sources for rule-ID
// literals, so an ID added anywhere without a docs page and a self-test
// fixture fails here rather than shipping.
func TestAllIDsCoversEveryEmittedID(t *testing.T) {
	var found []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if strings.Contains(filepath.ToSlash(path), "/testfix/") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range ruleLiteral.FindAllSubmatch(b, -1) {
			found = append(found, string(m[1]))
		}
		return nil
	})
	require.NoError(t, err)
	slices.Sort(found)
	found = slices.Compact(found)
	require.NotEmpty(t, found)
	for _, id := range found {
		assert.Contains(t, AllIDs(), id, "%s is emitted but not in AllIDs", id)
	}
	for _, id := range AllIDs() {
		assert.Contains(t, found, id, "%s is in AllIDs but nothing emits it", id)
	}
}

func TestOffRuleExemptionsDropped(t *testing.T) {
	files := map[string]string{"a.tokens.json": `{
  "plain": { "$type": "number", "$value": 1, ` + ext(`"exempt": { "token-unclassified": "legacy import" }`) + ` } }`}
	res := Run(load(t, files), nil, Options{})
	require.Len(t, res.Exemptions, 1)

	cfg := testConfig + "lint:\n  rules: { token-unclassified: off }\n"
	res = Run(loadWith(t, cfg, files), nil, Options{})
	assert.NotNil(t, res.Exemptions, "an empty list, never nil")
	assert.Empty(t, res.Exemptions, "an off rule's exemptions suppressed nothing")
}

// A rule that scanned nothing and gave no reason of its own is marked not
// applicable, so it can never read as a clean check.
func TestEmptyScanIsNotApplicable(t *testing.T) {
	res := Run(load(t, map[string]string{"a.tokens.json": `{ "gap": { "$type": "number", "$value": 4 } }`}), nil, Options{})
	reasons := map[string]string{}
	for _, tl := range res.Tallies {
		if tl.Items == 0 {
			assert.NotEmpty(t, tl.NotApplicable, tl.Rule)
		}
		reasons[tl.Rule] = tl.NotApplicable
	}
	assert.Equal(t, "no tokens in scope", reasons["literal-in-semantic"])
	assert.Equal(t, "no contrast invariants declared", reasons["contrast-below"], "a rule's own reason stays")
}
