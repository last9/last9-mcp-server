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

func TestGetLogsRawAggregateSkipsOutcomeSummary(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		rows := streamsResult([]summaryTestLine{{severity: "INFO", message: "served"}})
		items := rows["data"].(map[string]interface{})["result"].([]interface{})
		items[0].(map[string]interface{})["stream"].(map[string]interface{})["status_code"] = "200"
		_ = json.NewEncoder(w).Encode(map[string]any{"query_result": rows})
	}))
	t.Cleanup(srv.Close)

	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}},
			{"type": "aggregate", "aggregates": []interface{}{map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"}}, "groupby": map[string]interface{}{"attributes['status_code']": "status_code"}},
		},
		ResponseMode: logResponseModeRaw, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
	if _, ok := parseToolJSONResult(t, result)["l9_outcome_summary"]; ok {
		t.Fatal("aggregate response has l9_outcome_summary")
	}
}

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
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"resultType": "matrix", "result": []any{
				map[string]any{"metric": map[string]any{"outcome": "500", "count": "3"}, "values": []any{}},
			}}})
			return
		}
		rows := streamsResult([]summaryTestLine{{severity: "ERROR", message: "failed"}})
		items := rows["data"].(map[string]interface{})["result"].([]interface{})
		stream := items[0].(map[string]interface{})
		stream["stream"].(map[string]interface{})["status_code"] = "500"
		stream["stream"].(map[string]interface{})["ServiceName"] = "api"
		stream["values"].([]interface{})[0].([]interface{})[0] = "1699999999123456789"
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
		row := response["rows"].([]any)[0].(map[string]any)
		if row["time"] == "" {
			t.Fatal("raw row has no local time")
		}
		if row["time_utc"] != "2023-11-14T22:13:19.123456789Z" || row["labels"].(map[string]any)["ServiceName"] != "api" {
			t.Fatalf("raw row lost timestamp precision or labels: %#v", row)
		}
	} else {
		entry := response["rows"].([]any)[0].(map[string]any)
		if entry["time"] == "" {
			t.Fatal("service log has no local time")
		}
	}
	summary, _ := response["l9_outcome_summary"].(map[string]any)
	counts, _ := summary["counts"].(map[string]any)
	if counts["500"] != float64(3) {
		t.Fatalf("outcome summary = %#v, want 500 count 3", summary)
	}
}
