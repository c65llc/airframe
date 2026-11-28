// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/tokens"
)

// ModeKeys lists the mode keys that can change t's value, the ones the
// json emitter writes for it: every mode key of a token reachable from t
// through aliases, closed under compatible unions, sorted by (pair count,
// key). The base ("") is not among them.
func ModeKeys(s *tokens.Set, t *tokens.Token) []config.ModeKey { return modeKeys(s, t) }

// UnionKey joins two mode keys; ok is false when they set one axis to
// different values.
func UnionKey(a, b config.ModeKey) (config.ModeKey, bool) { return unionKey(a, b) }

// CompareKeys orders mode keys by pair count, then as strings, so the
// base ("") sorts first.
func CompareKeys(a, b config.ModeKey) int { return compareKeys(a, b) }
