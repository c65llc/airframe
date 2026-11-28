// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package css

import (
	"fmt"
	"strings"
	"testing"
)

func sheetOf(t *testing.T, src string) *Sheet {
	t.Helper()
	return mustParse(t, src)
}

func envString(e Env) string {
	return fmt.Sprintf("%v|%v", e.Attrs, e.Media)
}

func TestEnvsProduct(t *testing.T) {
	s := sheetOf(t, `
:root { --a: 1; }
:root[data-theme='dark'] { --a: 2; }
:root:not([data-theme="light"]) { --a: 3; }
@media (prefers-color-scheme: dark) { :root { --a: 4; } }
`)
	envs, err := Envs(s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"map[]|map[]",
		"map[]|map[prefers-color-scheme:dark]",
		"map[data-theme:*]|map[]",
		"map[data-theme:*]|map[prefers-color-scheme:dark]",
		"map[data-theme:dark]|map[]",
		"map[data-theme:dark]|map[prefers-color-scheme:dark]",
		"map[data-theme:light]|map[]",
		"map[data-theme:light]|map[prefers-color-scheme:dark]",
	}
	if len(envs) != len(want) {
		t.Fatalf("got %d envs, want %d", len(envs), len(want))
	}
	for i, e := range envs {
		if envString(e) != want[i] {
			t.Errorf("env %d = %s, want %s", i, envString(e), want[i])
		}
	}
	again, _ := Envs(s)
	for i := range envs {
		if envString(again[i]) != envString(envs[i]) {
			t.Fatal("not deterministic")
		}
	}
}

func TestEnvsAcrossSheetsAndPresenceOnly(t *testing.T) {
	a := sheetOf(t, `:root[data-x] { --a: 1; }`)
	b := sheetOf(t, `@media (pointer: coarse) { :root[data-b='v'], :root:not([data-y]) { --a: 2; } }`)
	envs, err := Envs(a, b)
	if err != nil {
		t.Fatal(err)
	}
	// data-b {unset,*,v} x data-x {unset,*} x data-y {unset,*} x pointer {unset,coarse}
	if len(envs) != 24 {
		t.Fatalf("got %d envs", len(envs))
	}
	if got := envString(envs[1]); got != "map[]|map[pointer:coarse]" {
		t.Errorf("env 1 = %s", got)
	}
	if got := envString(envs[2]); got != "map[data-y:*]|map[]" {
		t.Errorf("env 2 = %s", got)
	}
}

func TestEnvsSetToUnnamedValue(t *testing.T) {
	s := sheetOf(t, `:root[data-density='relaxed'] { --a: 1; }
@media (pointer: coarse) { :root:not([data-density]) { --a: 2; } }`)
	envs, err := Envs(s)
	if err != nil {
		t.Fatal(err)
	}
	neg, _ := ParseSelector(":root:not([data-density])")
	found := false
	for _, e := range envs {
		if e.Attrs["data-density"] == "*" && e.Media["pointer"] == "coarse" {
			found = true
			if neg.Matches(e) {
				t.Error(":not([data-density]) must not match a set attribute")
			}
		}
	}
	if !found {
		t.Error("no env with data-density=* and pointer=coarse")
	}
}

func TestEnvsNoDimensions(t *testing.T) {
	envs, err := Envs(sheetOf(t, `:root { --a: 1; }`))
	if err != nil || len(envs) != 1 || len(envs[0].Attrs) != 0 {
		t.Fatalf("%v %v", envs, err)
	}
}

func TestEnvsUnsupported(t *testing.T) {
	_, err := Envs(sheetOf(t, "\n\n.dark { --a: 1; }"))
	if err == nil || !strings.Contains(err.Error(), "tokens.css:3") || !strings.Contains(err.Error(), ".dark") || !strings.Contains(err.Error(), RuleUnsupportedSelector) {
		t.Errorf("selector error: %v", err)
	}
	_, err = Envs(sheetOf(t, "@media print { :root { --a: 1; } }"))
	if err == nil || !strings.Contains(err.Error(), "tokens.css:1") || !strings.Contains(err.Error(), "print") || !strings.Contains(err.Error(), RuleUnsupportedMedia) {
		t.Errorf("media error: %v", err)
	}
}

func TestEnvsTooMany(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, ":root[data-a%d='x'] { --v: 1; }\n", i)
		fmt.Fprintf(&b, ":root[data-a%d='y'] { --v: 1; }\n", i)
	}
	_, err := Envs(sheetOf(t, b.String())) // 3^12 = 531441
	if err == nil || !strings.Contains(err.Error(), RuleTooManyEnvironments) {
		t.Errorf("got %v", err)
	}
}
