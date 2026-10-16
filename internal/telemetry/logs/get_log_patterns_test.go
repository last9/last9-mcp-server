package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type logSearchStub struct {
	mu     sync.Mutex
	bodies []map[string]any
}

// serve answers each log search with the lines listed for the request's
// start second, so a test can give a baseline and a current window different
// content.
func (s *logSearchStub) serve(t *testing.T, linesByStart map[int64][]string, totals map[int64]int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.mu.Unlock()

		start := int64(body["start"].(float64))
		values := make([]any, 0)
		for i, line := range linesByStart[start] {
			values = append(values, []any{fmt.Sprint((start + int64(i)) * 1e9), line})
		}
		resp := map[string]any{
			"query_result": map[string]any{"status": "success", "data": map[string]any{
				"resultType": "streams",
				"result":     []any{map[string]any{"stream": map[string]any{"severity": "ERROR"}, "values": values}},
			}},
		}
		if total, ok := totals[start]; ok {
			resp["total_matching_lines"] = total
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *logSearchStub) requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.bodies...)
}

func repeatLines(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(line, i)
	}
	return out
}

func decodeToolJSON[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	var out T
	text := result.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, text)
	}
	return out
}

const (
	patternWindowStartISO = "2026-10-16T10:00:00Z"
	patternWindowEndISO   = "2026-10-16T11:00:00Z"
	patternWindowStart    = int64(1792144800)
)

func TestGetLogPatterns_ClustersSampleAndFoldsTailIntoOther(t *testing.T) {
	lines := append(repeatLines("cache miss for key %d", 3), repeatLines("timeout calling payments after %d ms", 5)...)
	lines = append(lines, "shutting down worker", "worker pool resized")
	stub := &logSearchStub{}
	srv := stub.serve(t, map[int64][]string{patternWindowStart: lines}, map[int64]int{patternWindowStart: 870})

	result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{
			ServiceName: "checkout", Env: "prod", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO, SampleSize: 10,
		},
		TopN: 2,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := decodeToolJSON[GetLogPatternsResponse](t, result)

	if got.Service != "checkout" || got.Env != "prod" {
		t.Errorf("scope = %q/%q", got.Service, got.Env)
	}
	if got.Window.SampledLines != 10 || !got.Window.SampleLimitReached {
		t.Errorf("window = %+v, want 10 sampled lines with the limit reached", got.Window)
	}
	if got.Window.TotalMatchingLines == nil || *got.Window.TotalMatchingLines != 870 {
		t.Errorf("total_matching_lines = %v, want 870", got.Window.TotalMatchingLines)
	}
	if got.TotalPatterns != 4 || len(got.Patterns) != 2 {
		t.Fatalf("total_patterns = %d, patterns = %+v; want 4 total, top 2 returned", got.TotalPatterns, got.Patterns)
	}
	top := got.Patterns[0]
	if top.Template != "timeout calling payments after <*> ms" || top.Count != 5 || top.Sample != "timeout calling payments after 0 ms" {
		t.Errorf("top pattern = %+v", top)
	}
	if top.FirstSeen != "2026-10-16T10:00:03Z" || top.LastSeen != "2026-10-16T10:00:07Z" || top.SeverityCounts["ERROR"] != 5 {
		t.Errorf("top pattern stats = %+v", top.LogPatternStats)
	}
	if got.Patterns[1].Template != "cache miss for key <*>" || got.Patterns[1].Count != 3 {
		t.Errorf("second pattern = %+v", got.Patterns[1])
	}
	if got.Other == nil || got.Other.Patterns != 2 || got.Other.Count != 2 {
		t.Errorf("other = %+v, want the 2 remaining single-line patterns", got.Other)
	}

	reqs := stub.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want one search", len(reqs))
	}
	if reqs[0]["limit"] != float64(10) {
		t.Errorf("limit = %v, want sample_size", reqs[0]["limit"])
	}
	pipeline, _ := json.Marshal(reqs[0]["pipeline"])
	for _, want := range []string{`"ServiceName","checkout"`, `"resources['deployment.environment']","prod"`} {
		if !strings.Contains(string(pipeline), want) {
			t.Errorf("pipeline %s missing %s", pipeline, want)
		}
	}
}

func TestGetLogPatterns_EmptyWindowIsAValidAnswer(t *testing.T) {
	stub := &logSearchStub{}
	srv := stub.serve(t, nil, nil)

	result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"patterns": []`) {
		t.Errorf("empty result must serve patterns as [], got:\n%s", text)
	}
	got := decodeToolJSON[GetLogPatternsResponse](t, result)
	if got.TotalPatterns != 0 || got.Other != nil || got.Window.SampledLines != 0 || got.Window.SampleLimitReached {
		t.Errorf("response = %+v, want an empty, complete window", got)
	}
	if reqs := stub.requests(); reqs[0]["limit"] != float64(defaultPatternSampleSize) {
		t.Errorf("limit = %v, want default sample size %d", reqs[0]["limit"], defaultPatternSampleSize)
	}
}

func TestGetLogPatterns_RejectsInvalidArgsBeforeQuerying(t *testing.T) {
	cases := map[string]GetLogPatternsArgs{
		"missing service":       {},
		"blank service":         {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "  "}},
		"sample over max":       {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", SampleSize: maxPatternSampleSize + 1}},
		"negative sample":       {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", SampleSize: -1}},
		"top_n over max":        {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a"}, TopN: maxPatternTopN + 1},
		"negative top_n":        {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a"}, TopN: -1},
		"negative lookback":     {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", LookbackMinutes: -5}},
		"bad index":             {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", Index: "nope"}},
		"start after end":       {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", StartTimeISO: patternWindowEndISO, EndTimeISO: patternWindowStartISO}},
		"unparseable timestamp": {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", StartTimeISO: "yesterday"}},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &logSearchStub{}
			srv := stub.serve(t, nil, nil)
			_, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
			if err == nil {
				t.Fatal("want an error")
			}
			if n := len(stub.requests()); n != 0 {
				t.Errorf("made %d requests before rejecting", n)
			}
		})
	}
}

func TestGetLogPatterns_AcceptsLimitsAtTheirMaximum(t *testing.T) {
	stub := &logSearchStub{}
	srv := stub.serve(t, nil, nil)
	_, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", SampleSize: maxPatternSampleSize},
		TopN:                maxPatternTopN,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if reqs := stub.requests(); reqs[0]["limit"] != float64(maxPatternSampleSize) {
		t.Errorf("limit = %v, want %d", reqs[0]["limit"], maxPatternSampleSize)
	}
}
