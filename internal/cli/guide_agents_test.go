// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/gittest"
	"github.com/c65llc/airframe/internal/mcp"
	"github.com/c65llc/airframe/internal/skills"
)

// guideExchange is one block of the agents guide that a server checks,
// named by the HTML comment above it: "<!-- mcp: call -->" and the
// "result" or "error" block after it, a "wire" block of request and
// response lines ("wire read-only" for a --read-only server), the
// "initialize" request, or the "audit" lines.
var guideMarker = regexp.MustCompile("(?s)<!-- mcp: ([a-z -]+?) -->\n```[a-z]+\n(.*?)```")

type guideExchange struct{ kind, body string }

// guideVariable is what the guide says varies from run to run: the audit
// line's session and duration, and the commit a ref names in the
// reader's repository. Nothing else is normalised.
var guideVariable = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`session=[0-9a-f]{8}`), "session=…"},
	{regexp.MustCompile(` ms=\d+ `), " ms=… "},
	{regexp.MustCompile(`"commit": "[0-9a-f]{40}"`), `"commit": "…"`},
}

func normalise(s string) string {
	for _, v := range guideVariable {
		s = v.re.ReplaceAllString(s, v.with)
	}
	return s
}

// excerptMatches reports whether got is want, where want may be cut at a
// line holding only "…": got then starts with what precedes that line and
// ends with what follows it.
func excerptMatches(want, got string) bool {
	head, tail, cut := strings.Cut(want, "\n…\n")
	if !cut {
		return want == got
	}
	return len(got) > len(head)+len(tail)+2 && strings.HasPrefix(got, head+"\n") && strings.HasSuffix(got, "\n"+tail)
}

func TestExcerptMatches(t *testing.T) {
	assert.True(t, excerptMatches("a\nb", "a\nb"))
	assert.True(t, excerptMatches("a\n…\nz", "a\nb\nc\nz"))
	assert.False(t, excerptMatches("a\n…\nz", "a\nz"), "an excerpt cuts something")
	assert.False(t, excerptMatches("a\n…\nz", "x\nb\nz"))
	assert.False(t, excerptMatches("a\nb", "a\nb\n"))
}

// rpcLine is one JSON-RPC response line, decoded as far as the guide
// checks it.
type rpcLine struct {
	ID     json.RawMessage `json:"id"`
	Result *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	} `json:"result"`
}

// serve runs airframe mcp (with args) in dir over the request lines, and
// returns each response line by its id, and stderr.
func serve(t *testing.T, dir string, requests []string, args ...string) (map[string]string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	in := strings.Join(requests, "\n") + "\n"
	code := Main(append([]string{"--root", dir, "mcp"}, args...), strings.NewReader(in), &stdout, &stderr)
	require.Equal(t, ExitOK, code, stderr.String())
	byID := map[string]string{}
	for _, l := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		var r rpcLine
		require.NoError(t, json.Unmarshal([]byte(l), &r), l)
		byID[string(r.ID)] = l
	}
	return byID, stderr.String()
}

// TestGuideAgentsMCP runs every MCP exchange the agents guide quotes
// against a real server, in the guide's order and in one session, over
// the reference guide's project formatted, built and committed as
// origin/main, and requires exactly the result it quotes. A --read-only
// server answers the read-only exchange, and the audit lines it quotes
// are the ones the session logged.
func TestGuideAgentsMCP(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "agents.md"))
	require.NoError(t, err)
	guide := string(data)
	var exchanges []guideExchange
	for _, m := range guideMarker.FindAllStringSubmatch(guide, -1) {
		exchanges = append(exchanges, guideExchange{m[1], strings.TrimSuffix(m[2], "\n")})
	}

	g := readReferenceGuide(t)
	r := gittest.New(t)
	r.Write(g.before)
	for _, args := range [][]string{{"fmt"}, {"build"}} {
		code, _, stderr := run(append([]string{"--root", r.Dir}, args...)...)
		require.Equal(t, ExitOK, code, stderr)
	}
	r.Commit("the design system")
	r.Git("update-ref", "refs/remotes/origin/main", "HEAD")

	// The session: initialize, then every call and wire request in the
	// guide's order. A call's id is 1000 plus its place; a wire exchange
	// keeps the id it quotes.
	type check struct {
		id, kind, want string
		name           string
	}
	var initialize string
	var requests, roRequests, audit []string
	var checks, roChecks []check
	called := map[string]bool{}
	for i := 0; i < len(exchanges); i++ {
		e := exchanges[i]
		switch e.kind {
		case "initialize":
			require.Empty(t, initialize, "the guide quotes one initialize")
			initialize = e.body
		case "call":
			require.Less(t, i+1, len(exchanges), "a call is followed by its result")
			next := exchanges[i+1]
			require.Contains(t, []string{"result", "error"}, next.kind, "a call is followed by its result: %s", e.body)
			i++
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			require.NoError(t, json.Unmarshal([]byte(e.body), &params), e.body)
			called[params.Name] = true
			id := fmt.Sprint(1000 + len(checks))
			line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "tools/call", "params": json.RawMessage(e.body)})
			require.NoError(t, err)
			requests = append(requests, string(line))
			checks = append(checks, check{id: id, kind: next.kind, want: next.body, name: params.Name})
		case "wire", "wire read-only":
			lines := strings.Split(e.body, "\n")
			require.Len(t, lines, 2, "a wire exchange is a request line and its response")
			var req struct {
				ID     json.RawMessage `json:"id"`
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			require.NoError(t, json.Unmarshal([]byte(lines[0]), &req))
			c := check{id: string(req.ID), kind: "wire", want: lines[1]}
			if e.kind == "wire" {
				called[req.Params.Name] = true
				requests = append(requests, lines[0])
				checks = append(checks, c)
			} else {
				roRequests = append(roRequests, lines[0])
				roChecks = append(roChecks, c)
			}
		case "audit":
			audit = append(audit, strings.Split(e.body, "\n")...)
		default:
			t.Fatalf("an mcp block of unknown kind %q", e.kind)
		}
	}
	require.NotEmpty(t, initialize)
	requests = append([]string{initialize}, requests...)
	roRequests = append([]string{initialize}, roRequests...)
	require.NotEmpty(t, roChecks)
	require.NotEmpty(t, audit)
	for _, tool := range mcp.Tools() {
		assert.True(t, called[tool.Name], "the guide shows a call of %s", tool.Name)
	}

	verify := func(byID map[string]string, cs []check) {
		t.Helper()
		ids := map[string]bool{}
		for _, c := range cs {
			require.False(t, ids[c.id], "id %s is used twice", c.id)
			ids[c.id] = true
			got, ok := byID[c.id]
			require.True(t, ok, "no response to id %s", c.id)
			if c.kind == "wire" {
				assert.Equal(t, c.want, got, "the guide quotes the wire exchange of id %s exactly", c.id)
				// A result quoted on the wire is also shown as the model
				// reads it.
				var r rpcLine
				require.NoError(t, json.Unmarshal([]byte(got), &r))
				if r.Result != nil {
					assert.Contains(t, guide, "```text\n"+r.Result.Content[0].Text+"\n```", "the guide shows the wire result of id %s as text", c.id)
				}
				continue
			}
			var r rpcLine
			require.NoError(t, json.Unmarshal([]byte(got), &r))
			require.NotNil(t, r.Result, got)
			require.NotEmpty(t, r.Result.Content, got)
			text := r.Result.Content[0].Text
			assert.True(t, excerptMatches(normalise(c.want), normalise(text)),
				"the guide quotes %s's result exactly:\n--- guide\n%s\n--- server\n%s", c.name, c.want, text)
			if c.kind == "error" {
				assert.True(t, r.Result.IsError, "%s: the guide quotes an error", c.name)
				assert.Len(t, r.Result.Content, 1, c.name)
				continue
			}
			assert.False(t, r.Result.IsError, "%s: %s", c.name, text)
			require.Len(t, r.Result.Content, 2, c.name)
			assert.Equal(t, mcp.UntrustedDataRule, r.Result.Content[1].Text, c.name)
			// structuredContent is the fenced JSON: an object as it is, an
			// array as hits; a sheet has none.
			payload, isJSON := strings.CutPrefix(text, "<untrusted-data source=\"airframe-project\" kind=\"json\">\n")
			if !isJSON {
				assert.Empty(t, r.Result.Structured, c.name)
				continue
			}
			payload = strings.TrimSuffix(payload, "\n</untrusted-data>")
			if strings.HasPrefix(payload, "[") {
				payload = `{"hits":` + payload + `}`
			}
			assert.JSONEq(t, payload, string(r.Result.Structured), c.name)
		}
	}
	byID, stderr := serve(t, r.Dir, requests)
	verify(byID, checks)
	roByID, roStderr := serve(t, r.Dir, roRequests, "--read-only")
	verify(roByID, roChecks)
	assert.True(t, strings.HasSuffix(strings.SplitN(roStderr, "\n", 2)[0], " (read-only)"), roStderr)
	assert.NotContains(t, roStderr, "tool=airframe_token_set", "a refused write is not a tool call")

	// The audit lines the guide quotes are lines the session logged, in
	// order.
	logged := strings.Split(normalise(stderr), "\n")
	at := 0
	for _, want := range audit {
		i := slices.Index(logged[at:], normalise(want))
		if !assert.GreaterOrEqual(t, i, 0, "the session logged %q\nafter line %d of:\n%s", want, at, stderr) {
			break
		}
		at += i + 1
	}
	assert.Regexp(t, `^airframe mcp: serving \S+\n`, stderr)
}

// TestGuideAgentsQuotesTheRealConfig: the guide's MCP client
// configuration is what skills mcp prints, and it names every agent that
// has one.
func TestGuideAgentsQuotesTheRealConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "agents.md"))
	require.NoError(t, err)
	guide := string(data)
	for _, c := range []struct {
		agent    string
		readOnly bool
		lang     string
	}{{"claude-code", false, "json"}, {"codex", true, "toml"}} {
		out, err := skills.MCPConfig(c.agent, c.readOnly)
		require.NoError(t, err)
		assert.Contains(t, guide, "```"+c.lang+"\n"+out+"```", c.agent)
	}
	for _, a := range skills.Agents {
		if _, err := skills.MCPConfig(string(a), false); err == nil {
			assert.Contains(t, guide, "`"+string(a)+"`", a)
		}
	}
	assert.Contains(t, guide, mcp.UntrustedDataRule)
}
