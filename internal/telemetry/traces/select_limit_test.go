package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/utils"
)

func filterThenSelect(sel map[string]interface{}) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"ServiceName", ""}}},
		sel,
	}
}

// The engine applies a final select's own limit only when it is positive and
// ignores the request limit, so 0, -1 or a missing limit returns every span.
func TestSanitizeTraceJSONQuery_FinalSelectLimitBounds(t *testing.T) {
	bad := map[string]map[string]interface{}{
		"limit 0":      {"type": "select", "order": []interface{}{"Duration"}, "limit": 0.0},
		"limit -1":     {"type": "select", "order": []interface{}{"Duration"}, "limit": -1.0},
		"limit 1001":   {"type": "select", "order": []interface{}{"Duration"}, "limit": 1001.0},
		"limit str":    {"type": "select", "order": []interface{}{"Duration"}, "limit": "10"},
		"limit frac":   {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.5},
		"offset -1":    {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0, "offset": -1.0},
		"offset 10001": {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0, "offset": 10001.0},
	}
	for name, sel := range bad {
		t.Run(name, func(t *testing.T) {
			err := SanitizeTraceJSONQuery(filterThenSelect(sel))
			if err == nil {
				t.Fatalf("expected rejection for %v", sel)
			}
			if !strings.Contains(err.Error(), "tracejson_query[1]") {
				t.Fatalf("error %q should name tracejson_query[1]", err)
			}
		})
	}
	good := map[string]map[string]interface{}{
		"limit 10":         {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0},
		"limit int":        {"type": "select", "order": []interface{}{"Duration"}, "limit": 1000},
		"offset":           {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0, "offset": 20.0},
		"labels, no limit": {"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"}},
	}
	for name, sel := range good {
		t.Run(name, func(t *testing.T) {
			if err := SanitizeTraceJSONQuery(filterThenSelect(sel)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFinalSelectLimitErrorExampleSatisfiesGetTracesSchema(t *testing.T) {
	query := []map[string]interface{}{{
		"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"}, "limit": 0.0,
	}}
	err := SanitizeTraceJSONQuery(query)
	if err == nil {
		t.Fatal("expected limit validation error")
	}
	match := regexp.MustCompile(`e\.g\. (\{.*\}); got`).FindStringSubmatch(err.Error())
	if len(match) != 2 {
		t.Fatalf("error has no copyable example: %q", err)
	}
	var example map[string]interface{}
	if err := json.Unmarshal([]byte(match[1]), &example); err != nil {
		t.Fatalf("decode example: %v", err)
	}

	rawSchema, err := json.Marshal(GetTracesInputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(map[string]interface{}{"tracejson_query": []interface{}{example}}); err != nil {
		t.Fatalf("limit recovery example does not satisfy get_traces schema: %v", err)
	}
}

type selectBodyRecorder struct {
	mu     sync.Mutex
	bodies []map[string]interface{}
}

func (r *selectBodyRecorder) handler(w http.ResponseWriter, req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	var body map[string]interface{}
	_ = json.Unmarshal(raw, &body)
	r.mu.Lock()
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
	_, _ = w.Write([]byte(traceAPIResponse(1)))
}

func lastStage(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	stages, _ := body["pipeline"].([]interface{})
	if len(stages) == 0 {
		t.Fatalf("request body has no pipeline: %v", body)
	}
	stage, _ := stages[len(stages)-1].(map[string]interface{})
	return stage
}

func TestGetTracesHandlerRunsSelectAsOneBoundedRequest(t *testing.T) {
	cases := []struct {
		name      string
		sel       map[string]interface{}
		argLimit  int
		wantLimit float64
	}{
		{"missing limit takes the request limit", map[string]interface{}{"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"}}, 25, 25},
		{"missing limit is capped at 1000", map[string]interface{}{"type": "select", "order": []interface{}{"Duration"}}, 5000, 1000},
		{"explicit limit is kept", map[string]interface{}{"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0}, 50, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &selectBodyRecorder{}
			server := httptest.NewServer(http.HandlerFunc(rec.handler))
			defer server.Close()

			handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
				TracejsonQuery: filterThenSelect(tc.sel),
				// 7 days: a plain filter would be split into many requests, each
				// returning its own top-N.
				StartTimeISO: "2026-01-01T00:00:00Z",
				EndTimeISO:   "2026-01-08T00:00:00Z",
				Limit:        tc.argLimit,
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if len(rec.bodies) != 1 {
				t.Fatalf("expected 1 request for a select pipeline, got %d", len(rec.bodies))
			}
			if got := lastStage(t, rec.bodies[0])["limit"]; got != tc.wantLimit {
				t.Fatalf("final select limit = %v, want %v", got, tc.wantLimit)
			}
		})
	}
}

func TestGetTracesHandlerCapsExplicitSelectResponse(t *testing.T) {
	for _, tc := range []struct {
		name            string
		selectLimit     int
		requestLimit    int
		configuredLimit int
		wantRows        int
	}{
		{name: "request limit caps select", selectLimit: 10, requestLimit: 3, wantRows: 3},
		{name: "configured limit caps select", selectLimit: 10, configuredLimit: 3, wantRows: 3},
		{name: "smaller select limit is preserved", selectLimit: 2, requestLimit: 3, wantRows: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(traceAPIResponse(tc.selectLimit)))
			}))
			defer server.Close()

			cfg := testChunkTracesConfig(server.URL)
			cfg.MaxGetTracesEntries = tc.configuredLimit
			result, _, err := NewGetTracesHandler(server.Client(), cfg)(
				context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
					TracejsonQuery: filterThenSelect(map[string]interface{}{
						"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"}, "limit": float64(tc.selectLimit),
					}),
					StartTimeISO: "2026-01-01T00:00:00Z",
					EndTimeISO:   "2026-01-01T00:05:00Z",
					Limit:        tc.requestLimit,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Data struct {
					Result []json.RawMessage `json:"result"`
				} `json:"data"`
				Guidance map[string]interface{} `json:"l9_guidance"`
			}
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
				t.Fatal(err)
			}
			if got := len(payload.Data.Result); got != tc.wantRows {
				t.Fatalf("returned %d rows, want %d", got, tc.wantRows)
			}
			if payload.Guidance == nil {
				t.Fatal("expected truncation guidance at the effective response limit")
			}
		})
	}
}

func TestGetTracesHandlerRejectsMalformedSelectResponse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		wantError bool
	}{
		{name: "error status without result", body: `{"status":"error","data":null}`, wantError: true},
		{name: "result is not an array", body: `{"status":"success","data":{"result":{"unexpected":1}}}`, wantError: true},
		{name: "empty result", body: `{"status":"success","data":{"result":[]}}`},
		{name: "one result", body: traceAPIResponse(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			result, _, err := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))(
				context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
					TracejsonQuery: filterThenSelect(map[string]interface{}{
						"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"}, "limit": 10.0,
					}),
					StartTimeISO: "2026-01-01T00:00:00Z",
					EndTimeISO:   "2026-01-01T00:05:00Z",
					Limit:        3,
				},
			)
			gotError := err != nil || result != nil && result.IsError
			if gotError != tc.wantError {
				t.Fatalf("error outcome = %t (result=%#v, err=%v), want %t", gotError, result, err, tc.wantError)
			}
			if !tc.wantError && result == nil {
				t.Fatal("valid result returned no tool response")
			}
		})
	}
}

func TestGetTracesHandlerUsesFinalSelectLimitForGuidance(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rows         int
		wantGuidance bool
	}{
		{name: "capped response", rows: maxSelectLimit, wantGuidance: true},
		{name: "response below cap", rows: 12, wantGuidance: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(traceAPIResponse(tc.rows)))
			}))
			defer server.Close()

			result, _, err := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))(
				context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
					TracejsonQuery: filterThenSelect(map[string]interface{}{
						"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"},
					}),
					StartTimeISO: "2026-01-01T00:00:00Z",
					EndTimeISO:   "2026-01-01T00:05:00Z",
					Limit:        5000,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
				t.Fatal(err)
			}
			_, gotGuidance := payload["l9_guidance"]
			if gotGuidance != tc.wantGuidance {
				t.Fatalf("l9_guidance present = %t, want %t", gotGuidance, tc.wantGuidance)
			}
			if tc.wantGuidance && strings.Contains(utils.GetTextContent(t, result), "5,000") {
				t.Fatal("guidance recommends a limit above the final select maximum")
			}
		})
	}
}
