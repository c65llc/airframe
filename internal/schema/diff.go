// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package schema

import _ "embed"

// diffSchema describes airframe diff --format json, an output rather than
// a document Airframe reads; it is served by airframe schema diff.
//
//go:embed diff.schema.json
var diffSchema []byte

func init() { sources["diff"] = diffSchema }
