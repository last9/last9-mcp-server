package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type capturedTraceQuery struct {
	start, end, limit string
	pipeline          []interface{}
}

func runGetTracesWindowAggregate(t *testing.T, window []interface{}) ([]capturedTraceQuery, string, error) {
	t.Helper()
	var mu sync.Mutex
	var captured []capturedTraceQuery
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
		captured = append(captured, capturedTraceQuery{start: q.Get("start"), end: q.Get("end"), limit: q.Get("limit"), pipeline: body.Pipeline})
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":{"resultType":"matrix","result":[{"metric":{"service":"checkout"},"values":[[1767225600,"42"]]}]}}`))
	}))
	defer server.Close()

	result, _, err := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
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

func TestGetTracesWholeDayWindowMatchesHourWindow(t *testing.T) {
	for _, tc := range []struct {
		days, hours []interface{}
	}{
		{days: []interface{}{"1", "days"}, hours: []interface{}{"24", "hours"}},
		{days: []interface{}{"7", "days"}, hours: []interface{}{"168", "hours"}},
		{days: []interface{}{"1", "day"}, hours: []interface{}{"24", "hours"}},
		{days: []interface{}{float64(7), "days"}, hours: []interface{}{"168", "hours"}},
	} {
		dayQueries, dayText, err := runGetTracesWindowAggregate(t, tc.days)
		if err != nil {
			t.Fatalf("%v: %v", tc.days, err)
		}
		hourQueries, hourText, err := runGetTracesWindowAggregate(t, tc.hours)
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

func TestGetTracesWholeDayWindowMatchesHourWindow_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetTracesHandler(http.DefaultClient, *cfg)
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.Add(-24 * time.Hour)

	run := func(window []interface{}) interface{} {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		began := time.Now()
		result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetTracesArgs{
			TracejsonQuery: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "last9-mcp"}}},
				{"type": "window_aggregate", "function": map[string]interface{}{"$count": []interface{}{}}, "as": "count", "window": window},
			},
			StartTimeISO: start.Format(time.RFC3339),
			EndTimeISO:   end.Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("%v: %v", window, err)
		}
		var body struct {
			Data interface{} `json:"data"`
		}
		if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &body); err != nil {
			t.Fatalf("%v: decode result: %v", window, err)
		}
		t.Logf("%v over %s..%s: %d series in %s: %v", window, start.Format(time.RFC3339), end.Format(time.RFC3339), seriesCount(body.Data), time.Since(began), body.Data)
		return body.Data
	}

	hours := run([]interface{}{"24", "hours"})
	days := run([]interface{}{"1", "days"})
	if !reflect.DeepEqual(days, hours) {
		t.Errorf("1 days returned %v, want the 24 hours result %v", days, hours)
	}
	if seriesCount(hours) == 0 {
		t.Log("no last9-mcp traces in the window; day/hour equality is vacuous")
	}
}

func seriesCount(data interface{}) int {
	m, _ := data.(map[string]interface{})
	result, _ := m["result"].([]interface{})
	return len(result)
}

func TestGetTracesRejectsDayWindowsThatCannotBecomeHours(t *testing.T) {
	for _, window := range [][]interface{}{
		{"384307168202282326", "days"},
		{"0", "days"},
		{"-1", "days"},
		{"1.5", "days"},
		{float64(1.5), "days"},
	} {
		captured, _, err := runGetTracesWindowAggregate(t, window)
		if err == nil {
			t.Errorf("%v: want error", window)
		}
		if len(captured) != 0 {
			t.Errorf("%v: want no upstream request, got %d", window, len(captured))
		}
	}
}
