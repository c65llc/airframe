// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package importer

import (
	"slices"

	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/diag"
)

// The diagnostic IDs the importer reports beyond those internal/css does.
// All are errors except RuleDuplicate and RuleFallbackDropped (warnings).
const (
	RuleUnmappedScope   = "import-unmapped-scope"
	RuleNameInvalid     = "import-name-invalid"
	RuleTypeConflict    = "import-type-conflict"
	RuleNoBase          = "import-no-base"
	RuleDuplicate       = "import-duplicate"
	RuleFallbackDropped = "import-fallback-dropped"
	RuleNotEquivalent   = "import-not-equivalent"
	RuleNothing         = "import-nothing"
)

// IDs lists every diagnostic ID an import can report, sorted. They are
// command findings, not lint rules.
func IDs() []string {
	ids := []string{
		css.RuleSyntax, css.RuleUnsupportedAtRule, css.RuleUnsupportedValue,
		css.RuleUnsupportedSelector, css.RuleUnsupportedMedia, css.RuleTooManyEnvironments,
		RuleUnmappedScope, RuleNameInvalid, RuleTypeConflict, RuleNoBase,
		RuleDuplicate, RuleFallbackDropped, RuleNotEquivalent, RuleNothing,
	}
	slices.Sort(ids)
	return ids
}

// Severity is the severity an import reports id at: warn for
// RuleDuplicate and RuleFallbackDropped, error for every other ID.
func Severity(id string) diag.Severity {
	if id == RuleDuplicate || id == RuleFallbackDropped {
		return diag.Warn
	}
	return diag.Error
}
