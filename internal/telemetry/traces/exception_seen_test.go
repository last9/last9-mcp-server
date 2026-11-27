package traces

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type promRangeRequest struct {
	Query     string  `json:"query"`
	Timestamp float64 `json:"timestamp"`
	Window    float64 `json:"window"`
	Step      int64   `json:"step"`
}

func decodeRangeRequest(t *testing.T, w http.ResponseWriter, r *http.Request) (promRangeRequest, bool) {
	t.Helper()
	var body promRangeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fixtureHTTPError(t, w, "decode range request: %v", err)
		return promRangeRequest{}, false
	}
	return body, true
}

const twoEnvRows = `[
	{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"value":[1768906800,"9"]},
	{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"staging"},"value":[1768906800,"4"]},
	{"metric":{"exception_type":"IOError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"value":[1768906800,"1"]}
]`

func TestGetExceptionsHandler_FirstAndLastSeenComeFromRangeData(t *testing.T) {
	start := exceptionTestWindow.start.Unix()
	end := exceptionTestWindow.end.Unix()
	var request promRangeRequest

	rows := runGetExceptions(t, getExceptionsTestRequest{InstantBody: twoEnvRows, RangeHandler: func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		request, ok = decodeRangeRequest(t, w, r)
		if !ok {
			return
		}
		io.WriteString(w, fmt.Sprintf(`[
			{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},
			 "values":[[%d,"5"],[%d,"0"],[%d,"2"],[%d,"1"],[%d,"0"]]},
			{"metric":{"exception_type":"IOError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},
			 "values":[[%d,"0"],[%d,"0"]]}
		]`, start, start+120, start+180, start+600, end, start+60, end))
	}})

	if !strings.Contains(request.Query, "sum by (exception_type, service_name, span_name, span_kind, env)") ||
		!strings.Contains(request.Query, "trace_endpoint_count{exception_type!=''}[1m]") {
		t.Errorf("range query must keep the instant query's grouping and filters with a one-step range: %s", request.Query)
	}
	if request.Timestamp != float64(end) || request.Window != 3600 || request.Step != 60 {
		t.Errorf("range request = %+v, want timestamp=end, window=3600, step=60", request)
	}

	byKey := map[string]map[string]any{}
	for _, row := range rows {
		byKey[fmt.Sprintf("%v/%v", row["exception_type"], row["deployment_environment"])] = row
	}

	prod := byKey["TypeError/prod"]
	if prod["first_seen"] != "2026-01-20T10:03:00Z" || prod["last_seen"] != "2026-01-20T10:10:00Z" {
		t.Errorf("prod first/last seen = %v / %v, want the first and last in-window steps with occurrences (a point at the window start covers only time before it)",
			prod["first_seen"], prod["last_seen"])
	}
	for _, key := range []string{"TypeError/staging", "IOError/prod"} {
		row := byKey[key]
		if row == nil {
			t.Fatalf("missing row %s in %v", key, rows)
		}
		if _, found := row["first_seen"]; found {
			t.Errorf("%s: first_seen must be absent without in-window occurrences: %#v", key, row)
		}
		if _, found := row["last_seen"]; found {
			t.Errorf("%s: last_seen must be absent without in-window occurrences: %#v", key, row)
		}
	}
}

func TestGetExceptionsHandler_FirstAndLastSeenNeedFinitePositiveCounts(t *testing.T) {
	start := exceptionTestWindow.start.Unix()
	for _, value := range []string{"NaN", "+Inf", "-Inf", "0", "-1", "invalid", "2"} {
		t.Run(value, func(t *testing.T) {
			rows := runGetExceptions(t, getExceptionsTestRequest{InstantBody: twoEnvRows, RangeHandler: func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, fmt.Sprintf(`[
					{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},
					 "values":[[%d,%q],[%d,"1"],[%d,%q]]}
				]`, start+60, value, start+120, start+180, value))
			}})

			first, last := "2026-01-20T10:02:00Z", "2026-01-20T10:02:00Z"
			if value == "2" {
				first, last = "2026-01-20T10:01:00Z", "2026-01-20T10:03:00Z"
			}
			if rows[0]["first_seen"] != first || rows[0]["last_seen"] != last {
				t.Errorf("sample %q: first/last seen = %v / %v, want %s / %s (only a finite positive count is an occurrence)",
					value, rows[0]["first_seen"], rows[0]["last_seen"], first, last)
			}
		})
	}
}

func TestGetExceptionsHandler_NormalizesPublicTimestampsToWholeSeconds(t *testing.T) {
	for _, fraction := range []time.Duration{0, 900 * time.Millisecond} {
		t.Run(fraction.String(), func(t *testing.T) {
			requestedWindow := exceptionWindow{
				start: exceptionTestWindow.start.Add(fraction),
				end:   exceptionTestWindow.end.Add(fraction),
			}
			window := exceptionWindow{start: requestedWindow.start.Truncate(time.Second), end: requestedWindow.end.Truncate(time.Second)}
			if window.start.Before(requestedWindow.start) {
				window.start = window.start.Add(time.Second)
			}
			first := window.start.Add(time.Minute)
			last := window.start.Add(2 * time.Minute)
			response := runGetExceptionsResponse(t, getExceptionsTestRequest{
				InstantBody: twoEnvRows,
				Window:      requestedWindow,
				RangeHandler: func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintf(w, `[{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"values":[[%f,"1"],[%f,"1"]]}]`,
						float64(first.UnixNano())/float64(time.Second), float64(last.UnixNano())/float64(time.Second))
				},
			})

			if response.StartTime != window.start.Format(time.RFC3339Nano) || response.EndTime != window.end.Format(time.RFC3339Nano) {
				t.Fatalf("reported window = %s / %s, want %s / %s", response.StartTime, response.EndTime, window.start.Format(time.RFC3339Nano), window.end.Format(time.RFC3339Nano))
			}
			row := response.Exceptions[0]
			if row["first_seen"] != first.Format(time.RFC3339Nano) || row["last_seen"] != last.Format(time.RFC3339Nano) {
				t.Fatalf("first/last seen = %v / %v, want %s / %s", row["first_seen"], row["last_seen"], first.Format(time.RFC3339Nano), last.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestGetExceptionsHandler_FirstAndLastSeenRangeFiltersMatchInstantQuery(t *testing.T) {
	var query string
	runGetExceptions(t, getExceptionsTestRequest{Args: GetExceptionsArgs{ServiceName: "last9-api", SpanName: new("GET /a"), Env: "prod"}, InstantBody: `[]`, RangeHandler: func(w http.ResponseWriter, r *http.Request) {
		request, ok := decodeRangeRequest(t, w, r)
		if !ok {
			return
		}
		query = request.Query
		io.WriteString(w, `[]`)
	}})
	if !strings.Contains(query, `trace_internal_count{service_name=~'last9\\-api', span_name=~'GET \\/a', env=~'prod', exception_type!=''}[1m]`) {
		t.Errorf("range query must carry the instant query's filters: %s", query)
	}
}

func TestGetExceptionsHandler_FirstAndLastSeenOmittedWhenRangeQueryFails(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"http 500": nil,
		"malformed body": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"not":"a series list"`)
		},
		"no data": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `[]`)
		},
	}
	for name, rangeHandler := range cases {
		t.Run(name, func(t *testing.T) {
			rows := runGetExceptions(t, getExceptionsTestRequest{InstantBody: twoEnvRows, RangeHandler: rangeHandler})
			if len(rows) != 3 {
				t.Fatalf("rows = %d, want 3: the range query must never fail the tool", len(rows))
			}
			for _, row := range rows {
				for _, field := range []string{"first_seen", "last_seen"} {
					if _, found := row[field]; found {
						t.Errorf("%s must be absent, never the window bounds: %#v", field, row)
					}
				}
				if row["fingerprint"] == nil {
					t.Errorf("identity fields must survive a range failure: %#v", row)
				}
			}
		})
	}
}

func TestExceptionSeenStep_BoundsPointsPerSeries(t *testing.T) {
	cases := []struct {
		window time.Duration
		want   time.Duration
	}{
		{time.Minute, time.Minute},
		{60 * time.Minute, time.Minute},
		{60*time.Minute + time.Second, 2 * time.Minute},
		{120 * time.Minute, 2 * time.Minute},
		{7 * 24 * time.Hour, 168 * time.Minute},
	}
	for _, tc := range cases {
		step := exceptionSeenStep(tc.window)
		if step != tc.want {
			t.Errorf("exceptionSeenStep(%v) = %v, want %v", tc.window, step, tc.want)
		}
		if points := (tc.window + step - 1) / step; points > exceptionSeenMaxPoints {
			t.Errorf("window %v at step %v gives %d points, over %d", tc.window, step, points, exceptionSeenMaxPoints)
		}
	}
}

func TestExceptionSeenStepBoundsMaximumDuration(t *testing.T) {
	for _, window := range []time.Duration{time.Hour, time.Duration(math.MaxInt64)} {
		step := exceptionSeenStep(window)
		points := window / step
		if window%step != 0 {
			points++
		}
		if points > exceptionSeenMaxPoints {
			t.Fatalf("window %s uses step %s and requests %d points; want at most %d", window, step, points, exceptionSeenMaxPoints)
		}
	}
}

func TestExceptionSeenPlanRejectsUnrepresentableWindow(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()

	start := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	_, err := fetchExceptionSeen(t.Context(), srv.Client(), exceptionTestConfig(srv.URL), exceptionSeenQuery{
		Start: start,
		End:   start.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("one-hour window: %v", err)
	}
	if requests != 1 {
		t.Fatalf("one-hour window requests = %d, want 1", requests)
	}

	_, err = fetchExceptionSeen(t.Context(), srv.Client(), exceptionTestConfig(srv.URL), exceptionSeenQuery{
		Start: time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "range exceeds") {
		t.Fatalf("unrepresentable window error = %v, want range exceeds error", err)
	}
	if requests != 1 {
		t.Fatalf("requests after unrepresentable window = %d, want 1", requests)
	}
}

func TestGetExceptionsHandlerRejectsUnrepresentableOccurrenceWindow(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer srv.Close()

	start := "1000-01-01T00:00:00Z"
	_, _, err := NewGetExceptionsHandler(srv.Client(), exceptionTestConfig(srv.URL))(
		t.Context(),
		&mcp.CallToolRequest{},
		GetExceptionsArgs{StartTimeISO: &start, EndTimeISO: "9999-01-01T00:00:00Z"},
	)
	if err == nil || !strings.Contains(err.Error(), "range exceeds") {
		t.Fatalf("handler error = %v, want range exceeds error", err)
	}
	if requests != 0 {
		t.Fatalf("upstream requests = %d, want 0", requests)
	}
}
