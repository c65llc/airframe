// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package docs embeds the rule pages, one Markdown file per rule ID, so
// `airframe explain <rule>` works offline and the published site and the
// binary cannot disagree.
package docs

import (
	"embed"
	"io/fs"
)

//go:embed rules/*.md
var rules embed.FS

// Rule returns the Markdown page of the rule with the given ID.
func Rule(id string) (string, bool) {
	if !fs.ValidPath(id) {
		return "", false
	}
	b, err := rules.ReadFile("rules/" + id + ".md")
	if err != nil {
		return "", false
	}
	return string(b), true
}
