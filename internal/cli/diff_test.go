// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/gittest"
	"github.com/c65llc/airframe/internal/schema"
)

const diffConfig = `version: 1
content: ds
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ":root[data-theme='dark']"}}
platforms:
  web: {emit: css}
`

const diffBefore = `{
  "gray": {
    "$type": "color",
    "50": {"$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "900": {"$value": {"colorSpace": "oklch", "components": [0.2, 0.012, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "surface": {"$type": "color", "$value": "{gray.50}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink": {"$type": "color", "$value": "{gray.900}", "$description": "Body text.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "legacy": {"$type": "color", "$value": "{gray.900}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "old-gap": {"$type": "dimension", "$value": {"value": 8, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "gone": {"$type": "dimension", "$value": {"value": 3, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}
}
`

const diffBeforeDark = `{
  "$extensions": {"com.c65llc.airframe": {"mode": "appearance:dark"}},
  "surface": {"$value": "{gray.900}"}
}
`

const diffAfter = `{
  "gray": {
    "$type": "color",
    "50": {"$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "700": {"$value": {"colorSpace": "oklch", "components": [0.38, 0.01, 250], "alpha": 0.5}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "900": {"$value": {"colorSpace": "oklch", "components": [0.2, 0.012, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "surface": {"$type": "color", "$value": "{gray.50}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink": {"$type": "color", "$value": "{gray.900}", "$description": "Body text | prose.", "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "legacy": {"$type": "color", "$value": "{gray.900}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic", "retired": true}}},
  "gap": {"$type": "dimension", "$value": {"value": 8, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}
}
`

const diffAfterDark = `{
  "$extensions": {"com.c65llc.airframe": {"mode": "appearance:dark"}},
  "surface": {"$value": "{gray.700}"}
}
`

// diffRepo is a repository whose project lives in design/: tag v1 is the
// before state and main (HEAD) the after state.
func diffRepo(t *testing.T) (*gittest.Repo, string, string, string) {
	t.Helper()
	r := gittest.New(t)
	r.Write(map[string]string{
		"README.md":                             "not the project\n",
		"design/airframe.yaml":                  diffConfig,
		"design/ds/tokens/base.tokens.json":     diffBefore,
		"design/ds/themes/dark.tokens.json":     diffBeforeDark,
		"design-other/ds/tokens/x.tokens.json":  "not json",
		"design/ds/generated/css/ignored.txt":   "x\n",
		"design/web/app.css":                    ".a { color: var(--ink); }\n",
		"design/ds/tokens/README.not-tokens.md": "notes\n",
	})
	v1 := r.Commit("before")
	r.Git("tag", "v1")
	r.Write(map[string]string{
		"design/ds/tokens/base.tokens.json": diffAfter,
		"design/ds/themes/dark.tokens.json": diffAfterDark,
	})
	v2 := r.Commit("after")
	return r, filepath.Join(r.Dir, "design"), v1, v2
}

func TestDiffFormats(t *testing.T) {
	_, dir, v1, v2 := diffRepo(t)
	for _, format := range []string{"text", "md", "json"} {
		t.Run(format, func(t *testing.T) {
			code, stdout, stderr := run("--root", dir, "diff", "v1", "main", "--format", format)
			require.Equal(t, ExitOK, code, stderr)
			got := strings.NewReplacer(v1, "<v1>", v2, "<v2>").Replace(stdout)
			golden := filepath.Join("testdata", "diff."+format+".golden")
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
			if format == "json" {
				var doc any
				require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
				assert.Empty(t, schema.Validate("diff", doc), "the output validates against airframe schema diff")
				doc.(map[string]any)["added"] = []any{map[string]any{"name": "x"}}
				assert.NotEmpty(t, schema.Validate("diff", doc), "the schema is not vacuous")
			}
		})
	}
	// text is the default format.
	_, plain, _ := run("--root", dir, "diff", "v1", "main")
	_, text, _ := run("--root", dir, "diff", "v1", "main", "--format", "text")
	assert.Equal(t, text, plain)

	code, _, stderr := run("--root", dir, "diff", "v1", "--format", "yaml")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "unknown format yaml")
}

func TestDiffWorkingTree(t *testing.T) {
	r, dir, _, _ := diffRepo(t)
	// Clean: HEAD and the working tree agree.
	code, stdout, stderr := run("--root", dir, "diff", "HEAD")
	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, "HEAD → working tree: no token changes\n", stdout)

	// An uncommitted edit shows; nothing is staged, committed or checked out.
	r.Write(map[string]string{"design/ds/themes/dark.tokens.json": diffBeforeDark})
	statusBefore := r.Git("status", "--porcelain")
	code, stdout, stderr = run("--root", dir, "diff", "HEAD", "--format", "json")
	require.Equal(t, ExitOK, code, stderr)
	var doc struct {
		To      map[string]any `json:"to"`
		Changed []struct {
			Name string `json:"name"`
		} `json:"changed"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.Equal(t, map[string]any{"workingTree": true}, doc.To)
	require.Len(t, doc.Changed, 1)
	assert.Equal(t, "surface", doc.Changed[0].Name)
	assert.Equal(t, statusBefore, r.Git("status", "--porcelain"), "diff changed nothing in the repository")

	// From a subdirectory of the project, with no --root, the project is
	// still found.
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	require.NoError(t, os.Chdir(filepath.Join(dir, "ds", "tokens")))
	code, stdout, stderr = run("diff", "v1")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "v1 → working tree: 2 added, 2 removed")
}

func TestDiffRefInjection(t *testing.T) {
	r, dir, v1, _ := diffRepo(t)
	r.Git("branch", "feature/with-slash", v1)
	r.Git("tag", "-a", "-m", "annotated", "v1-annotated", v1)

	// Every ordinary spelling of a ref works.
	for _, ref := range []string{"feature/with-slash", "v1", "v1-annotated", v1[:8], "HEAD~1", "main~1", "HEAD^"} {
		code, stdout, stderr := run("--root", dir, "diff", ref, "main")
		require.Equal(t, ExitOK, code, "%s: %s", ref, stderr)
		assert.Contains(t, stdout, ref+" → main: 2 added, 2 removed, 1 possibly renamed, 3 changed", ref)
	}

	// An option-looking ref never reaches git: as a flag it is a usage
	// error, and after -- it is refused by name.
	out := filepath.Join(t.TempDir(), "x")
	code, _, stderr := run("--root", dir, "diff", "--output="+out)
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "flag provided but not defined")
	for _, ref := range []string{"--output=" + out, "-p", "-"} {
		code, _, stderr = run("--root", dir, "diff", "--", ref)
		assert.Equal(t, ExitUsage, code, ref)
		assert.Contains(t, stderr, `starts with "-"`, ref)
		code, _, stderr = run("--root", dir, "diff", "v1", "--", ref)
		assert.Equal(t, ExitUsage, code, ref)
		assert.Contains(t, stderr, `starts with "-"`, ref)
	}
	_, err := os.Stat(out)
	assert.ErrorIs(t, err, os.ErrNotExist, "git never wrote --output")

	// A ref that names nothing, or no commit, exits 2 naming it.
	for _, ref := range []string{"nope", "HEAD~9", "v1:design/airframe.yaml", "ma\x00in"} {
		code, _, stderr = run("--root", dir, "diff", ref)
		assert.Equal(t, ExitUsage, code, ref)
		assert.Contains(t, stderr, "airframe diff: ref ", ref)
	}
}

func TestDiffContentMoved(t *testing.T) {
	r := gittest.New(t)
	r.Write(map[string]string{
		"airframe.yaml":                diffConfig,
		"ds/tokens/base.tokens.json":   diffBefore,
		"ds/themes/dark.tokens.json":   diffBeforeDark,
		"unrelated/x.tokens.json":      "{}",
		"another/place/ignored.tokens": "{}",
	})
	r.Commit("before")
	r.Git("tag", "v1")
	// The content root moves to tokens/, and one value changes on the way.
	r.Remove("ds")
	r.Write(map[string]string{
		"airframe.yaml":                    strings.Replace(diffConfig, "content: ds", "content: tokens", 1),
		"tokens/tokens/base.tokens.json":   diffBefore,
		"tokens/themes/dark.tokens.json":   diffAfterDark,
		"tokens/tokens/extra.tokens.json":  `{"gray": {"$type": "color", "700": {"$value": {"colorSpace": "oklch", "components": [0.38, 0.01, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}}}`,
		"tokens/generated/css/stale.notes": "x",
	})
	r.Commit("moved")

	code, stdout, stderr := run("--root", r.Dir, "diff", "v1", "HEAD")
	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, `v1 → HEAD: 1 added, 0 removed, 0 possibly renamed, 1 changed

added (1):
  gray-700  color  primitive  #3e4348 oklch(38% 0.01 250)

changed (1):
  surface
    value appearance:dark: #12171b oklch(20% 0.012 250) → #3e4348 oklch(38% 0.01 250)
`, stdout, "each side loaded with its own content root")

	// And in reverse: HEAD's config read at HEAD, v1's at v1.
	code, stdout, stderr = run("--root", r.Dir, "diff", "HEAD", "v1")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "HEAD → v1: 0 added, 1 removed, 0 possibly renamed, 1 changed")
}

func TestDiffLoadFailure(t *testing.T) {
	r := gittest.New(t)
	r.Write(map[string]string{"README.md": "no project yet\n"})
	r.Commit("empty")
	r.Git("tag", "empty")
	r.Write(map[string]string{"airframe.yaml": diffConfig, "ds/tokens/base.tokens.json": diffBefore})
	r.Commit("project")
	r.Write(map[string]string{"ds/tokens/base.tokens.json": "{ not json"})
	r.Commit("broken")
	r.Git("tag", "broken")

	code, stdout, stderr := run("--root", r.Dir, "diff", "empty", "main~1")
	assert.Equal(t, ExitFindings, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "the project does not load at empty (")
	assert.Contains(t, stderr, "config-missing")

	code, _, stderr = run("--root", r.Dir, "diff", "main~1", "broken")
	assert.Equal(t, ExitFindings, code)
	assert.Contains(t, stderr, "the project does not load at broken (")
	assert.Contains(t, stderr, "ds/tokens/base.tokens.json")
}

func TestDiffNotWorkTree(t *testing.T) {
	gittest.New(t) // skips without git
	dir := t.TempDir()
	code, _, _ := run("--root", dir, "init")
	require.Equal(t, ExitOK, code)
	code, _, stderr := run("--root", dir, "diff", "HEAD")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "is not inside a git work tree")
}

func TestDiffNoGit(t *testing.T) {
	_, dir, _, _ := diffRepo(t)
	t.Setenv("PATH", "")
	code, stdout, stderr := run("--root", dir, "diff", "v1")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "airframe diff: git is not installed")
}

func TestDiffExitCode(t *testing.T) {
	_, dir, _, _ := diffRepo(t)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"diff", "v1", "main"}, ExitOK},
		{[]string{"diff", "v1", "main", "--exit-code"}, ExitFindings},
		{[]string{"diff", "main", "HEAD", "--exit-code"}, ExitOK},
		{[]string{"diff", "HEAD", "--exit-code"}, ExitOK},
		{[]string{"diff", "v1", "--exit-code", "--format", "md"}, ExitFindings},
	} {
		code, _, stderr := run(append([]string{"--root", dir}, tc.args...)...)
		assert.Equal(t, tc.want, code, "%v: %s", tc.args, stderr)
	}
	code, _, stderr := run("--root", dir, "diff")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "diff takes one or two git refs")
	code, _, _ = run("--root", dir, "diff", "a", "b", "c")
	assert.Equal(t, ExitUsage, code)
}
