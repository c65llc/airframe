// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c65llc/airframe/internal/gitfs"
	"github.com/c65llc/airframe/internal/report"
)

// Error results are not fenced, so their text must never carry project
// content: a token name, a component file, a parser's quote of a line or a
// "did you mean" that names a token could hold instructions. Only messages
// the server wrote, which quote at most the caller's own arguments, reach
// the model as they are; every other error is replaced by a fixed sentence
// for its kind. The audit line keeps the original, for the person reading
// the log.

// toolError is a message the server wrote for the caller.
type toolError string

func (e toolError) Error() string { return string(e) }

// failf is a toolError.
func failf(format string, args ...any) error { return toolError(fmt.Sprintf(format, args...)) }

// public is the text of err that may go to the model.
func public(err error) string {
	var (
		te          toolError
		refErr      *gitfs.RefError
		notLoaded   *report.NotLoadedError
		notWorkTree *report.NotWorkTreeError
		bothMissing *report.BothMissingError
	)
	switch {
	case errors.As(err, &te):
		return te.Error()
	case errors.As(err, &refErr):
		return refErr.Error() // it quotes the caller's ref, and says what is wrong with it
	case errors.As(err, &notLoaded):
		return notLoaded.Error() + "; airframe_lint reports why"
	case errors.As(err, &notWorkTree):
		return "the project is not inside a git work tree; airframe_diff compares git refs"
	case errors.As(err, &bothMissing):
		return "neither ref has airframe.yaml at the project's directory; check the refs, or the server's project root"
	case errors.Is(err, gitfs.ErrNoGit):
		return "git is not installed (no git on PATH); airframe_diff needs it"
	}
	return "the server could not read or write the project's files; check that the project directory is readable and writable, then retry"
}

// argsError is the text for arguments that do not decode: the argument
// named, never Go's type names.
func argsError(err error) error {
	var typeErr *json.UnmarshalTypeError
	msg := err.Error()
	switch {
	case errors.As(err, &typeErr) && typeErr.Field != "":
		return failf("invalid arguments: %s has the wrong type; tools/list gives each tool's input schema", typeErr.Field)
	case strings.HasPrefix(msg, "json: unknown field "):
		return failf("invalid arguments: unknown argument %s; tools/list gives each tool's input schema", strings.TrimPrefix(msg, "json: unknown field "))
	}
	return failf("invalid arguments: they must be a JSON object; tools/list gives each tool's input schema")
}
