package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const parityDefinition = `{
	"name":"Gateway",
	"variables":[{"target":"env","type":"label","source":"env","regex":"prod.*","include_all":true,"all_value":".*"}],
	"links":[{"title":"Docs","url":"https://example.test"}],
	"panels":[{
		"id":"logs-panel","name":"Request logs","version":1,
		"visualization":{"type":"logs","logs_config":{"columns":["timestamp","body","service","severity"],"row_limit":1000,"sort_order":"desc","severity_coloring":false}},
		"queries":[{"name":"A","type":"range","telemetry":"logs","query_type":"log_ql","expr":"{env=~\"$env\"}"}],
		"field_overrides":[{"matcher":{"type":"regex","value":"status.*"},"properties":{"hidden":true,"thresholds":[{"value":500,"color":"red","colorTarget":"background"}],"data_links":[{"title":"Trace","url":"https://example.test/${__data.fields.trace_id}"}]}}],
		"transformations":[{"id":"organize","options":{"excludeByName":{"Time":true}}}],
		"data_links":[{"title":"Details","url":"https://example.test/${__value.raw}"}]
	},{"id":"section-panel","name":"Details","collapsed":true,"visualization":{"type":"section"}}]
}`

func parityDashboardServer(t *testing.T) *httptest.Server {
	t.Helper()
	var stored DashboardRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&stored); err != nil {
				t.Error(err)
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			assertParityJSON(t, stored.Dashboard)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(stored); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func assertParityJSON(t *testing.T, got json.RawMessage) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(parityDefinition), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("native dashboard fields changed: got %s", got)
	}
}

func assertParityResult(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	text := result.Content[0].(*mcp.TextContent)
	var response DashboardRequest
	if err := json.Unmarshal([]byte(text.Text), &response); err != nil {
		t.Fatal(err)
	}
	assertParityJSON(t, response.Dashboard)
}

func TestDashboardNativeParityRoundTrip(t *testing.T) {
	server := parityDashboardServer(t)
	config := testDashboardConfig(server.URL)
	request := DashboardRequest{Dashboard: json.RawMessage(parityDefinition)}
	create, _, err := NewCreateDashboardHandler(server.Client(), config)(context.Background(), nil, CreateDashboardArgs{DashboardRequest: request})
	if err != nil {
		t.Fatal(err)
	}
	assertParityResult(t, create)
	update, _, err := NewUpdateDashboardHandler(server.Client(), config)(context.Background(), nil, UpdateDashboardArgs{ID: "dashboard-id", DashboardRequest: request})
	if err != nil {
		t.Fatal(err)
	}
	assertParityResult(t, update)
	read, _, err := NewGetDashboardHandler(server.Client(), config)(context.Background(), nil, GetDashboardArgs{ID: "dashboard-id"})
	if err != nil {
		t.Fatal(err)
	}
	assertParityResult(t, read)
}
