// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/report"
)

// writeUsages writes u as usages' text output.
func writeUsages(w io.Writer, u report.Usages) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\ntier: %s\ncomponent: %s\n", u.Name, orNone(u.Tier), orNone(u.Component))
	if len(u.References) == 0 && len(u.AliasedBy) == 0 {
		b.WriteString("no usages\n")
		_, _ = io.WriteString(w, b.String())
		return
	}
	if len(u.References) > 0 {
		b.WriteString("references:\n")
	}
	for _, r := range u.References {
		fmt.Fprintf(&b, "  %s  %s:%d:%d  %s", r.Platform, r.File, r.Line, r.Col, r.Raw)
		if r.Via != consumer.Direct {
			fmt.Fprintf(&b, "  (via %s)", r.Via)
		}
		switch {
		case r.Shared:
			b.WriteString("  [shared]")
		case r.Owner == nil:
		case *r.Owner:
			b.WriteString("  [owner]")
		default:
			b.WriteString("  [not owner]")
		}
		b.WriteString("\n")
	}
	if len(u.AliasedBy) > 0 {
		b.WriteString("aliased by:\n")
	}
	for _, a := range u.AliasedBy {
		fmt.Fprintf(&b, "  %s  %s  %s:%d\n", a.Name, a.Key, a.File, a.Line)
	}
	_, _ = io.WriteString(w, b.String())
}

func runUsages(inv *invocation) int {
	if len(inv.args) != 1 {
		return inv.usageError("usages takes one token name")
	}
	p, t, code := inv.loadToken(inv.args[0])
	if t == nil {
		return code
	}
	platform := inv.value("platform")
	if platform != "" && !slices.ContainsFunc(p.Config.Platforms, func(pl config.Platform) bool { return pl.Name == platform }) {
		return inv.usageError("unknown platform %s", platform)
	}
	u, err := report.DescribeUsages(p, t, platform)
	if err != nil {
		return inv.fail(fmt.Errorf("scan consumers: %w", err))
	}
	if *inv.bools["json"] {
		enc := json.NewEncoder(inv.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(u) // a write error is Main's to report
		return ExitOK
	}
	writeUsages(inv.stdout, u)
	return ExitOK
}
