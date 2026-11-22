package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type capturedLogQuery struct {
	start, end, limit string
	pipeline          []interface{}
}

func runGetLogsWindowAggregate(t *testing.T, window []interface{}) ([]capturedLogQuery, string, error) {
	t.Helper()
	var mu sync.Mutex
	var captured []capturedLogQuery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Pipeline []interface{} `json:"pipeline"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		q := r.URL.Query()
		mu.Lock()
		captured = append(captured, capturedLogQuery{start: q.Get("start"), end: q.Get("end"), limit: q.Get("limit"), pipeline: body.Pipeline})
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":{"resultType":"matrix","result":[{"metric":{"service":"checkout"},"values":[[1767225600,"42"]]}]}}`))
	}))
	defer server.Close()

	result, _, err := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}},
			{"type": "window_aggregate", "function": map[string]interface{}{"$count": []interface{}{}}, "as": "count", "window": window},
		},
		StartTimeISO: "2026-01-01T00:00:00Z",
		EndTimeISO:   "2026-01-01T00:30:00Z",
	})
	if err != nil {
		return captured, "", err
	}
	return captured, result.Content[0].(*mcp.TextContent).Text, nil
}

func TestGetLogsWholeDayWindowMatchesHourWindow(t *testing.T) {
	for _, tc := range []struct {
		days, hours []interface{}
	}{
		{days: []interface{}{"1", "days"}, hours: []interface{}{"24", "hours"}},
		{days: []interface{}{"7", "days"}, hours: []interface{}{"168", "hours"}},
		{days: []interface{}{"1", "day"}, hours: []interface{}{"24", "hours"}},
		{days: []interface{}{float64(7), "days"}, hours: []interface{}{"168", "hours"}},
	} {
		dayQueries, dayText, err := runGetLogsWindowAggregate(t, tc.days)
		if err != nil {
			t.Fatalf("%v: %v", tc.days, err)
		}
		hourQueries, hourText, err := runGetLogsWindowAggregate(t, tc.hours)
		if err != nil {
			t.Fatalf("%v: %v", tc.hours, err)
		}
		if len(dayQueries) != 1 {
			t.Fatalf("%v: want 1 upstream request, got %d", tc.days, len(dayQueries))
		}
		if !reflect.DeepEqual(dayQueries, hourQueries) {
			t.Errorf("%v sent %+v, want the %v request %+v", tc.days, dayQueries, tc.hours, hourQueries)
		}
		if got := dayQueries[0]; got.start != "1767225600" || got.end != "1767227400" {
			t.Errorf("%v: scan widened to start=%s end=%s", tc.days, got.start, got.end)
		}
		if dayText != hourText {
			t.Errorf("%v result differs from %v:\n%s\n%s", tc.days, tc.hours, dayText, hourText)
		}
	}
}

func TestGetLogsRejectsDayWindowsThatCannotBecomeHours(t *testing.T) {
	for _, window := range [][]interface{}{
		{"384307168202282326", "days"},
		{"0", "days"},
		{"-1", "days"},
		{"1.5", "days"},
		{float64(1.5), "days"},
	} {
		captured, _, err := runGetLogsWindowAggregate(t, window)
		if err == nil {
			t.Errorf("%v: want error", window)
		}
		if len(captured) != 0 {
			t.Errorf("%v: want no upstream request, got %d", window, len(captured))
		}
	}
}
