// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package importer

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
)

// TestSeverityMatchesReports holds Severity, which the docs pages are
// checked against, to the severities an import actually reports.
func TestSeverityMatchesReports(t *testing.T) {
	cfg := loadConfig(t, smallConfig)
	r := ImportSources(cfg, []Source{{File: "theme.css", Data: []byte(
		":root { --a: 1px; --a: 2px; --b: var(--a, 3px); --c: calc(1px + 2px) }\n")}})
	var rules []string
	for _, d := range r.Diagnostics {
		assert.Equal(t, Severity(d.Rule), d.Severity, "%s", d.Rule)
		rules = append(rules, d.Rule)
	}
	require.Subset(t, rules, []string{RuleDuplicate, RuleFallbackDropped, "import-unsupported-value"})
	assert.Equal(t, diag.Error, Severity(RuleNotEquivalent))
	assert.True(t, slices.IsSorted(IDs()))
	assert.Len(t, IDs(), 14)
}
