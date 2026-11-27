// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package format

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/project"
)

func TestTokensReservedKeyOrder(t *testing.T) {
	in := `{"a":{"zeta":1,"$extensions":{"x":1},"$description":"d","$value":"#fff","$type":"color","alpha":2}}`
	got, err := Tokens([]byte(in))
	require.NoError(t, err)
	want := "{\n  \"a\": {\n    \"$type\": \"color\",\n    \"$value\": \"#fff\",\n    \"$description\": \"d\",\n    \"$extensions\": {\n      \"x\": 1\n    },\n    \"zeta\": 1,\n    \"alpha\": 2\n  }\n}\n"
	assert.Equal(t, want, string(got))
}

func TestTokensPreservesGroupOrder(t *testing.T) {
	in := `{"z":{"$description":"g","$type":"color","b":{"$value":"1"},"a":{"$value":"2"}},"m":{"$value":"3"}}`
	got, err := Tokens([]byte(in))
	require.NoError(t, err)
	s := string(got)
	assert.Less(t, strings.Index(s, `"z"`), strings.Index(s, `"m"`))
	assert.Less(t, strings.Index(s, `"b"`), strings.Index(s, `"a"`))
	assert.Less(t, strings.Index(s, `"$description"`), strings.Index(s, `"$type"`), "group-level reserved keys stay put")
}

func loadMinimal(t *testing.T) *project.Project {
	t.Helper()
	p, ds, err := project.Load(os.DirFS(filepath.Join("..", "..", "testdata", "minimal")))
	require.NoError(t, err)
	require.Empty(t, ds)
	return p
}

func TestFormatIdempotent(t *testing.T) {
	p := loadMinimal(t)
	require.NotEmpty(t, Paths(p))
	for _, f := range Paths(p) {
		once, err := File(f, p.Sources[f])
		require.NoError(t, err, f)
		twice, err := File(f, once)
		require.NoError(t, err, f)
		assert.Equal(t, string(once), string(twice), f)
	}
	assert.Empty(t, Project(p), "testdata/minimal is already formatted")
}

func TestYAMLKeepsComments(t *testing.T) {
	in := "# head\nversion: 1  # trailing\nmodes:\n    a:   [x, y]\n    # inner\n    b: 2\n"
	got, err := YAML([]byte(in))
	require.NoError(t, err)
	s := string(got)
	for _, c := range []string{"# head", "# trailing", "# inner"} {
		assert.Contains(t, s, c)
	}
	assert.Contains(t, s, "\n  a: [x, y]\n")
	assert.True(t, strings.HasSuffix(s, "b: 2\n") && !strings.HasSuffix(s, "\n\n"))
}

func TestYAMLMultiDocumentIsError(t *testing.T) {
	_, err := YAML([]byte("a: 1\n---\nb: 2\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple YAML documents are not supported")
}

func TestYAMLCommentsOnlyUnchanged(t *testing.T) {
	got, err := YAML([]byte("# just a note\r\n# another\n\n\n"))
	require.NoError(t, err)
	assert.Equal(t, "# just a note\n# another\n", string(got))
}

func TestFormatCRLFOnlyIsFormatted(t *testing.T) {
	p := loadMinimal(t)
	crlf := strings.ReplaceAll(string(p.Sources["design-system/tokens/semantic.tokens.json"]), "\n", "\r\n")
	got, err := Tokens([]byte(crlf))
	require.NoError(t, err)
	assert.NotContains(t, string(got), "\r")

	p.Sources["design-system/tokens/semantic.tokens.json"] = []byte(crlf)
	assert.Empty(t, Project(p))
}

func TestProjectReturnsOnlyChanged(t *testing.T) {
	p := loadMinimal(t)
	f := "design-system/tokens/semantic.tokens.json"
	p.Sources[f] = []byte(`{"a":{"$value":"1","$type":"color"}}`)
	got := Project(p)
	require.Len(t, got, 1)
	assert.Contains(t, got, f)
	var _ fs.FS = p.FS
}
