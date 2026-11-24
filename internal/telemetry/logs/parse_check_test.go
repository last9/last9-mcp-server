package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// parseCheckBodies holds the three body shapes from issue 609: multi-line
// key=value blocks (a header line, then one pair per line), XML-tag text,
// JSON and plain text. Synthetic data only.
func parseCheckBodies() []string {
	var bodies []string
	for i := 0; i < 19; i++ {
		bodies = append(bodies, fmt.Sprintf("batch report\nkey1=alpha%d\nkey2=beta", i))
	}
	for i := 0; i < 9; i++ {
		bodies = append(bodies, "<event><state>report</state></event>")
	}
	for i := 0; i < 8; i++ {
		bodies = append(bodies, `{"msg":"report sent"}`)
	}
	for i := 0; i < 8; i++ {
		bodies = append(bodies, "report sent to downstream")
	}
	return bodies
}

// newParseCheckServer fakes the backend behaviour seen in issue 609: a logfmt
// parse stage drops multi-line bodies and leaves the other shapes with empty
// labels; a pipeline without a parse stage counts every row.
func newParseCheckServer(t *testing.T, failControl bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	bodies := parseCheckBodies()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointLogsQueryRange && r.URL.Path != constants.EndpointLogSearch {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		requests.Add(1)
		var req struct {
			Pipeline []map[string]any `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		parsed := false
		for _, stage := range req.Pipeline {
			if stage["type"] == "parse" {
				parsed = true
			}
		}
		if !parsed && failControl {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		count := 0
		for _, body := range bodies {
			if parsed && strings.Contains(body, "\n") {
				continue
			}
			count++
		}
		metric := map[string]any{"count": count}
		if parsed {
			metric["key1"] = ""
			metric["key2"] = ""
		}
		response := map[string]any{
			"data": map[string]any{
				"resultType": "matrix",
				"result":     []any{map[string]any{"metric": metric, "values": []any{}}},
			},
		}
		if r.URL.Path == constants.EndpointLogSearch {
			response = map[string]any{"query_result": map[string]any{"status": "success", "data": response["data"]}}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func logfmtCountPipeline(extra ...map[string]interface{}) []map[string]interface{} {
	pipeline := []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{
			map[string]interface{}{"$eq": []interface{}{"ServiceName", "batch"}},
			map[string]interface{}{"$containsWords": []interface{}{"Body", "report"}},
		}}},
		{"type": "parse", "parser": "logfmt", "field": "Body", "labels": map[string]interface{}{"key1": "key1", "key2": "key2"}},
	}
	pipeline = append(pipeline, extra...)
	return append(pipeline, map[string]interface{}{
		"type": "aggregate",
		"aggregates": []interface{}{
			map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"},
		},
		"groupby": map[string]interface{}{"attributes['key1']": "key1", "attributes['key2']": "key2"},
	})
}

func runParseCheckQuery(t *testing.T, server *httptest.Server, pipeline []map[string]interface{}) map[string]any {
	t.Helper()
	return runParseCheckQueryWithConfig(t, server, testLogsConfig(server.URL), pipeline)
}

func runParseCheckQueryWithConfig(t *testing.T, server *httptest.Server, cfg models.Config, pipeline []map[string]interface{}) map[string]any {
	t.Helper()
	handler := NewGetLogsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: pipeline,
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:30:00Z",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return out
}

func TestGetLogsReportsRowsDroppedByParseStage(t *testing.T) {
	server, requests := newParseCheckServer(t, false)
	out := runParseCheckQuery(t, server, logfmtCountPipeline())

	check, ok := out["l9_parse_check"].(map[string]any)
	if !ok {
		t.Fatalf("expected l9_parse_check, got %v", out)
	}
	want := map[string]float64{"rows_before_parse": 44, "rows_after_parse": 25, "unparsed_rows": 19}
	for key, value := range want {
		if check[key] != value {
			t.Errorf("%s = %v, want %v", key, check[key], value)
		}
	}
	if check["parser"] != "logfmt" {
		t.Errorf("parser = %v, want logfmt", check["parser"])
	}
	if note, _ := check["note"].(string); !strings.Contains(note, "regexp") {
		t.Errorf("note should point at a regexp parse, got %q", note)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected the query plus one pre-parse count, got %d requests", got)
	}
}

func TestGetLogsReportsRowsDroppedByParseStageOnLogSearchAPI(t *testing.T) {
	server, requests := newParseCheckServer(t, false)
	out := runParseCheckQueryWithConfig(t, server, apiModeCfg(server.URL), logfmtCountPipeline())

	check, ok := out["l9_parse_check"].(map[string]any)
	if !ok {
		t.Fatalf("expected l9_parse_check, got %v", out)
	}
	if check["rows_before_parse"] != float64(44) || check["rows_after_parse"] != float64(25) || check["unparsed_rows"] != float64(19) {
		t.Errorf("l9_parse_check = %v, want 44 before, 25 after, 19 unparsed", check)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected the search plus one pre-parse count, got %d requests", got)
	}
}

func TestGetLogsParseCheckSilentWhenNoRowsDropped(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointLogsQueryRange {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":{"resultType":"matrix","result":[{"metric":{"key1":"a","count":7},"values":[]},{"metric":{"key1":"b","count":3},"values":[]}]}}`))
	}))
	defer server.Close()
	out := runParseCheckQuery(t, server, logfmtCountPipeline())
	if _, ok := out["l9_parse_check"]; ok {
		t.Fatalf("expected no l9_parse_check when counts match, got %v", out["l9_parse_check"])
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected the query plus one pre-parse count, got %d requests", got)
	}
}

func TestGetLogsParseCheckSkipsFiltersAfterParse(t *testing.T) {
	server, requests := newParseCheckServer(t, false)
	pipeline := logfmtCountPipeline(map[string]interface{}{
		"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"attributes['key1']", "alpha1"}},
	})
	out := runParseCheckQuery(t, server, pipeline)
	if _, ok := out["l9_parse_check"]; ok {
		t.Fatalf("a filter after the parse drops rows on purpose; got %v", out["l9_parse_check"])
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("expected no pre-parse count, got %d requests", got)
	}
}

func TestGetLogsParseCheckReportsFailedPreParseCount(t *testing.T) {
	server, _ := newParseCheckServer(t, true)
	out := runParseCheckQuery(t, server, logfmtCountPipeline())

	check, ok := out["l9_parse_check"].(map[string]any)
	if !ok {
		t.Fatalf("expected l9_parse_check, got %v", out)
	}
	if check["rows_after_parse"] != float64(25) {
		t.Errorf("rows_after_parse = %v, want 25", check["rows_after_parse"])
	}
	if _, ok := check["rows_before_parse"]; ok {
		t.Errorf("rows_before_parse must be absent when its count failed, got %v", check["rows_before_parse"])
	}
	if errText, _ := check["error"].(string); errText == "" {
		t.Errorf("expected an error naming the failed pre-parse count, got %v", check)
	}
	data, _ := out["data"].(map[string]any)
	if rows, _ := data["result"].([]any); len(rows) != 1 {
		t.Errorf("the parsed result must still be served, got %v", out["data"])
	}
}
