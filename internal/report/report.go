// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package report builds the documents the read commands print with --json
// (show, usages, rules, lint and diff), so that the command line and the
// MCP server, which return the same documents, share one code path. It
// gathers; it does not print, except for the JSON encoding both share.
package report
