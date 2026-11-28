// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package consumer

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c65llc/airframe/internal/config"
)

// config.AdapterNames is what retired.yaml spellings are keyed by; it must
// name exactly the adapters this package has.
func TestAdapterNamesMatchConfig(t *testing.T) {
	var got []string
	for _, a := range adapters {
		if !slices.Contains(got, a.name) {
			got = append(got, a.name)
		}
	}
	assert.ElementsMatch(t, config.AdapterNames, got)
}
