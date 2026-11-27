// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package ojson

import (
	"errors"
	"strings"
	"testing"
)

func TestRoundTripPreservesOrder(t *testing.T) {
	n, err := Parse([]byte(`{"b":1,"a":{"z":true,"y":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := string(Marshal(n, "  "))
	want := "{\n  \"b\": 1,\n  \"a\": {\n    \"z\": true,\n    \"y\": null\n  }\n}\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLineNumbers(t *testing.T) {
	n, err := Parse([]byte("{\n\"a\": 1,\n\"b\": 2\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if n.Line != 1 || n.Fields["a"].Line != 2 || n.Fields["b"].Line != 3 {
		t.Fatalf("lines: %d %d %d", n.Line, n.Fields["a"].Line, n.Fields["b"].Line)
	}
}

func TestDuplicateKeyIsError(t *testing.T) {
	_, err := Parse([]byte(`{"a":1,"a":2}`))
	if err == nil || !strings.Contains(err.Error(), `duplicate key "a"`) || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v", err)
	}
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 1 {
		t.Fatalf("not a SyntaxError with line: %v", err)
	}
}

func TestNumbersRoundTrip(t *testing.T) {
	n, err := Parse([]byte(`[0.13, 36, 1e-3]`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(Marshal(n, "")), "[0.13,36,0.001]\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEmptyContainersAndEscapes(t *testing.T) {
	n, err := Parse([]byte(`{"o":{},"a":[],"s":"a<b\n\"x\""}`))
	if err != nil {
		t.Fatal(err)
	}
	got := string(Marshal(n, "  "))
	want := "{\n  \"o\": {},\n  \"a\": [],\n  \"s\": \"a<b\\n\\\"x\\\"\"\n}\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, in := range []string{"", "{", "{\"a\":}", "[1,]", "{} {}", "{\n\n\"a\" 1}"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
	_, err := Parse([]byte("{\n\n\"a\" 1}"))
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 3 {
		t.Fatalf("want line 3, got %v", err)
	}
}
