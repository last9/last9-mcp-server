package logs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsChunkBudget(t *testing.T) {
	for _, tc := range []struct {
		window   time.Duration
		chunks   int
		rejected bool
	}{
		{23 * time.Hour, 23, false},
		{24 * time.Hour, 24, false},
		{24*time.Hour + time.Millisecond, 25, true},
		{25 * time.Hour, 25, true},
		{48 * time.Hour, 48, true},
		{168 * time.Hour, 168, true},
	} {
		t.Run(tc.window.String(), func(t *testing.T) {
			rec := newRequestRecorder()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.add(r.URL.Query())
				_, _ = w.Write([]byte(streamsAPIResponse(nil)))
			}))
			defer server.Close()
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			result, _, err := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))(
				context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
					LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}}}},
					StartTimeISO: start.Format(time.RFC3339Nano),
					EndTimeISO:   start.Add(tc.window).Format(time.RFC3339Nano),
					Limit:        1,
				})
			if tc.rejected {
				if got := rec.count(); got != 0 {
					t.Errorf("rejected plan sent %d upstream requests", got)
				}
				if err == nil || result != nil {
					t.Fatalf("want refusal without data, got result=%v err=%v", result, err)
				}
				for _, fragment := range []string{fmt.Sprintf("%d chunks", tc.chunks), "maximum 24", "lookback_minutes", "start_time_iso", "end_time_iso", "Do not retry unchanged"} {
					if !strings.Contains(err.Error(), fragment) {
						t.Errorf("missing %q in error: %v", fragment, err)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("allowed plan failed: %v", err)
				}
				if got := rec.count(); got != tc.chunks {
					t.Errorf("got %d requests, want %d", got, tc.chunks)
				}
			}
		})
	}
}

func TestGetLogsChunkBudgetExemptsSingleRequestPaths(t *testing.T) {
	for _, searchAPI := range []bool{false, true} {
		t.Run(fmt.Sprintf("search_api=%v", searchAPI), func(t *testing.T) {
			rec := newRequestRecorder()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.add(r.URL.Query())
				body := streamsAPIResponse(nil)
				if searchAPI {
					body = `{"query_result":` + body + `}`
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			cfg := testLogsConfig(server.URL)
			cfg.UseLogSearchAPI = searchAPI
			pipeline := []map[string]interface{}{{"type": "filter"}}
			if !searchAPI {
				pipeline = append(pipeline, map[string]interface{}{"type": "aggregate", "aggregates": []interface{}{map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"}}})
			}
			_, err := fetchLogJSONQuery(context.Background(), server.Client(), cfg, pipeline, 0, int64(168*time.Hour/time.Millisecond), GetLogsArgs{LogjsonQuery: pipeline})
			if err != nil {
				t.Fatal(err)
			}
			if got := rec.count(); got != 1 {
				t.Fatalf("got %d requests, want 1", got)
			}
		})
	}
}
