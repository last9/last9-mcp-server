package logs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func logPatternToolCalls(scope LogPatternScopeArgs) map[string]func(srv *httptest.Server) (*mcp.CallToolResult, error) {
	return map[string]func(srv *httptest.Server) (*mcp.CallToolResult, error){
		"get_log_patterns": func(srv *httptest.Server) (*mcp.CallToolResult, error) {
			result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{LogPatternScopeArgs: scope})
			return result, err
		},
		"get_emerging_log_patterns": func(srv *httptest.Server) (*mcp.CallToolResult, error) {
			result, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetEmergingLogPatternsArgs{LogPatternScopeArgs: scope})
			return result, err
		},
	}
}

// Both pattern tools link to the logs search they sampled; the emerging tool
// links its current window, the one being triaged.
func TestLogPatternTools_LinkTheSampledLogsSearch(t *testing.T) {
	scope := LogPatternScopeArgs{ServiceName: "checkout", Env: "prod", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO}
	for name, call := range logPatternToolCalls(scope) {
		t.Run(name, func(t *testing.T) {
			srv := (&logSearchStub{}).serve(t, nil, nil)
			result, err := call(srv)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			link := referenceURL(t, result)
			if link == "" {
				t.Fatal("expected reference_url in result meta")
			}
			parsed := parseRelativeURL(t, link)
			query := parsed.Query()
			if !strings.HasSuffix(parsed.Path, "/logs") || query.Get("from") != fmt.Sprint(patternWindowStart) || query.Get("to") != fmt.Sprint(patternWindowStart+3600) {
				t.Errorf("reference_url = %s, want the logs view over the current window", link)
			}
			for _, want := range []string{`"ServiceName","checkout"`, `"resources['deployment.environment']","prod"`} {
				if !strings.Contains(query.Get("pipeline"), want) {
					t.Errorf("pipeline %s missing %s", query.Get("pipeline"), want)
				}
			}
		})
	}
}

func TestLogPatternTools_OmitSourceLinkWhenIndexCannotBeResolved(t *testing.T) {
	scope := LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO, Index: "physical_index:missing"}
	for name, call := range logPatternToolCalls(scope) {
		t.Run(name, func(t *testing.T) {
			search := (&logSearchStub{}).handler(map[int64][]string{patternWindowStart: {"timeout while calling db"}}, nil)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/logs_settings/physical_indexes" {
					_, _ = io.Copy(io.Discard, r.Body)
					_, _ = w.Write([]byte(`{"properties":[]}`))
					return
				}
				search(w, r)
			}))
			t.Cleanup(srv.Close)
			result, err := call(srv)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if result.IsError {
				t.Fatalf("handler returned a tool error: %s", result.Content[0].(*mcp.TextContent).Text)
			}
			if got := referenceURL(t, result); got != "" {
				t.Fatalf("expected no reference_url for an unresolvable index, got %q", got)
			}
		})
	}
}

func TestRankLogPatterns_BreaksTiesByTemplateAndSplitsPastTopN(t *testing.T) {
	type item struct {
		template string
		count    int
	}
	byCountDesc := func(a, b item) int { return b.count - a.count }
	name := func(i item) string { return i.template }
	join := func(xs []item) string {
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			out = append(out, x.template)
		}
		return strings.Join(out, " ")
	}

	for _, tc := range []struct {
		topN              int
		wantTop, wantRest string
	}{
		{topN: 3, wantTop: "c d a", wantRest: "b"},
		{topN: 4, wantTop: "c d a b", wantRest: ""},
		{topN: 5, wantTop: "c d a b", wantRest: ""},
	} {
		// The tied "a" and "b" arrive out of template order, so the tie-break must reorder them.
		items := []item{{"b", 1}, {"c", 5}, {"a", 1}, {"d", 3}}
		top, rest := rankLogPatterns(items, tc.topN, name, byCountDesc)
		if got := join(top); got != tc.wantTop {
			t.Errorf("topN=%d: top = %q, want %q", tc.topN, got, tc.wantTop)
		}
		if got := join(rest); got != tc.wantRest {
			t.Errorf("topN=%d: rest = %q, want %q", tc.topN, got, tc.wantRest)
		}
	}
}
