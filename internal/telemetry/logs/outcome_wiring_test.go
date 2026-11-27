package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetServiceLogsRawWithoutStatusFilterSkipsDiscovery(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"query_result": streamsResult(nil)})
	}))
	t.Cleanup(srv.Close)

	_, _, err := NewGetServiceLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName: "api", ResponseMode: logResponseModeRaw,
		StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want only the raw log query", got)
	}
}

func TestGetLogsRawAddsLocalTimestampAndOutcomeSummary(t *testing.T) {
	srv := outcomeWiringServer(t)
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), timezoneRequest(), GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}},
		ResponseMode: logResponseModeRaw, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRawOutcomeWiring(t, parseToolJSONResult(t, result), true)
}

func TestGetServiceLogsRawAddsLocalTimestampAndOutcomeSummary(t *testing.T) {
	srv := outcomeWiringServer(t)
	result, _, err := NewGetServiceLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), timezoneRequest(), GetServiceLogsArgs{
		ServiceName: "api", ResponseMode: logResponseModeRaw,
		HTTPStatusCode: "500", HTTPStatusField: "attributes['status_code']",
		StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRawOutcomeWiring(t, parseToolJSONResult(t, result), false)
}

func outcomeWiringServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query []map[string]any `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(body.Query) > 0 && body.Query[len(body.Query)-1]["type"] == "aggregate" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"resultType": "streams", "result": []any{
				map[string]any{"stream": map[string]any{"outcome": "500", "count": "3"}},
			}}})
			return
		}
		rows := streamsResult([]summaryTestLine{{severity: "ERROR", message: "failed"}})
		items := rows["data"].(map[string]interface{})["result"].([]interface{})
		items[0].(map[string]interface{})["stream"].(map[string]interface{})["status_code"] = "500"
		_ = json.NewEncoder(w).Encode(map[string]any{"query_result": rows})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func timezoneRequest() *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{clientTimezoneMetaKey: "Asia/Kolkata"}}}
}

func assertRawOutcomeWiring(t *testing.T, response map[string]any, streams bool) {
	t.Helper()
	if streams {
		data := response["data"].(map[string]any)
		item := data["result"].([]any)[0].(map[string]any)
		row := item["values"].([]any)[0].([]any)
		if row[2].(map[string]any)["ts_local"] == "" {
			t.Fatal("raw row has no ts_local")
		}
	} else {
		entry := response["logs"].([]any)[0].(map[string]any)
		if entry["ts_local"] == "" {
			t.Fatal("service log has no ts_local")
		}
	}
	summary, _ := response["l9_outcome_summary"].(map[string]any)
	counts, _ := summary["counts"].(map[string]any)
	if counts["500"] != float64(3) {
		t.Fatalf("outcome summary = %#v, want 500 count 3", summary)
	}
}
