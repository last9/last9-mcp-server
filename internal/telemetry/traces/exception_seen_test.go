package traces

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type promRangeRequest struct {
	Query     string `json:"query"`
	Timestamp int64  `json:"timestamp"`
	Window    int64  `json:"window"`
	Step      int64  `json:"step"`
}

func decodeRangeRequest(t *testing.T, r *http.Request) promRangeRequest {
	t.Helper()
	var body promRangeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode range request: %v", err)
	}
	return body
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

	rows := runGetExceptions(t, GetExceptionsArgs{}, twoEnvRows, func(w http.ResponseWriter, r *http.Request) {
		request = decodeRangeRequest(t, r)
		io.WriteString(w, fmt.Sprintf(`[
			{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},
			 "values":[[%d,"5"],[%d,"0"],[%d,"2"],[%d,"1"],[%d,"0"]]},
			{"metric":{"exception_type":"IOError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},
			 "values":[[%d,"0"],[%d,"0"]]}
		]`, start, start+120, start+180, start+600, end, start+60, end))
	})

	if !strings.Contains(request.Query, "sum by (exception_type, service_name, span_name, span_kind, env)") ||
		!strings.Contains(request.Query, "trace_endpoint_count{exception_type!=''}[1m]") {
		t.Errorf("range query must keep the instant query's grouping and filters with a one-step range: %s", request.Query)
	}
	if request.Timestamp != end || request.Window != 3600 || request.Step != 60 {
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

func TestGetExceptionsHandler_FirstAndLastSeenRangeFiltersMatchInstantQuery(t *testing.T) {
	var query string
	runGetExceptions(t, GetExceptionsArgs{ServiceName: "last9-api", SpanName: "GET /a", Env: "prod"}, `[]`, func(w http.ResponseWriter, r *http.Request) {
		query = decodeRangeRequest(t, r).Query
		io.WriteString(w, `[]`)
	})
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
			rows := runGetExceptions(t, GetExceptionsArgs{}, twoEnvRows, rangeHandler)
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
