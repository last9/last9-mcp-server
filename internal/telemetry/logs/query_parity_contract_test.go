package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsForwardsSupportedFieldSyntax(t *testing.T) {
	tests := []struct {
		name  string
		stage string
	}{
		{"simple JSON key", `{"type":"parse","parser":"json","field":"Body","labels":{"request_id":"id"}}`},
		{"quoted literal JSON key", "{\"type\":\"parse\",\"parser\":\"json\",\"field\":\"Body\",\"labels\":{\"`request-id`\":\"id\"}}"},
		{"indexed JSON parse path", `{"type":"parse","parser":"json","field":"Body","labels":{"items[0].name":"first_item"}}`},
		{"indexed JSON filter path", `{"type":"filter","query":{"$and":[{"$eq":["items[0].name","first"]}]}}`},
		{"named regexp capture", `{"type":"parse","parser":"regexp","field":"Body","pattern":"status=(?P<code>[0-9]+)"}`},
		{"numbered regexp captures", `{"type":"parse","parser":"regexp","field":"Body","pattern":"method=(GET) status=([0-9]+)","labels":{"$1":"method","$2":"code"}}`},
		{"default logfmt delimiter", `{"type":"parse","parser":"logfmt","field":"Body"}`},
		{"custom logfmt delimiter", `{"type":"parse","parser":"logfmt","field":"Body","delimiter":":"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stage map[string]any
			if err := json.Unmarshal([]byte(tt.stage), &stage); err != nil {
				t.Fatal(err)
			}
			requests := make(chan []map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Pipeline []map[string]any `json:"pipeline"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode query: %v", err)
					http.Error(w, "invalid query", http.StatusBadRequest)
					return
				}
				select {
				case requests <- body.Pipeline:
				default:
					t.Error("unexpected extra request")
				}
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			}))
			defer server.Close()

			scope := map[string]any{"type": "filter", "query": map[string]any{"$eq": []any{"ServiceName", "example"}}}
			handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery: []map[string]any{scope, stage},
				StartTimeISO: "2026-01-01T00:00:00Z", EndTimeISO: "2026-01-01T00:01:00Z",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || result.IsError {
				t.Fatalf("expected a successful call, got %v", result)
			}
			select {
			case pipeline := <-requests:
				if len(pipeline) != 2 || !reflect.DeepEqual(pipeline[1], stage) {
					t.Fatalf("expected forwarded stage %v, got pipeline %v", stage, pipeline)
				}
			default:
				t.Fatal("query was not forwarded")
			}
		})
	}
}

func TestLogDiscoveryHintsSelectLiteralBodyKeys(t *testing.T) {
	server := bodySamplingServer(t, `{"status":"success","data":[]}`, []string{
		`{"http.status":"literal","http":{"status":"nested"},"request-id":"hyphen","request_id":"underscore"}`,
	}, nil)
	defer server.Close()

	scope := map[string]any{"type": "filter", "query": map[string]any{"$eq": []any{"ServiceName", "example"}}}
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]any{scope}, LookbackMinutes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	attrs := decodeLogAttributes(t, result)
	for _, tt := range []struct{ name, source string }{
		{"request_id", "request_id"}, {"http.status", "`http.status`"}, {"request-id", "`request-id`"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			attr := attrByName(attrs, tt.name)
			if attr == nil {
				t.Fatalf("missing discovered top-level key %q", tt.name)
			}
			var stages []map[string]any
			if err := json.Unmarshal([]byte(attr.Hint), &stages); err != nil {
				t.Fatal(err)
			}
			if len(stages) < 2 {
				t.Fatalf("incomplete parse/filter hint: %s", attr.Hint)
			}
			labels, _ := stages[0]["labels"].(map[string]any)
			if _, ok := labels[tt.source]; !ok {
				t.Fatalf("hint must read top-level key %q, got %v", tt.source, labels)
			}
			if _, err := prepareLogJSONQuery(append([]map[string]any{scope}, stages...), "logjson_query"); err != nil {
				t.Fatalf("discovery returned an unusable hint: %v", err)
			}
		})
	}
}
