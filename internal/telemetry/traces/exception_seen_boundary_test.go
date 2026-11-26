package traces

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExceptionSeenKeepsBucketsInsideRequestedWindow(t *testing.T) {
	start := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	key := exceptionSeriesKey(map[string]string{
		"exception_type": "TypeError", "service_name": "web", "span_name": "View",
		"span_kind": "SPAN_KIND_SERVER", "env": "prod",
	})
	for _, tc := range []struct {
		name       string
		window     time.Duration
		occurrence time.Time
		value      string
		wantSeen   bool
	}{
		{name: "divisible window excludes start", window: 60 * time.Minute, occurrence: start},
		{name: "divisible window includes end", window: 60 * time.Minute, occurrence: start.Add(60 * time.Minute), value: "1", wantSeen: true},
		{name: "non-divisible window excludes pre-window evidence", window: 61 * time.Minute, occurrence: start.Add(-30 * time.Second), value: "1"},
		{name: "non-divisible window includes final tail", window: 61 * time.Minute, occurrence: start.Add(60*time.Minute + 30*time.Second), value: "1", wantSeen: true},
		{name: "invalid sample is not an occurrence", window: 61 * time.Minute, occurrence: start.Add(30 * time.Minute), value: "+Inf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			end := start.Add(tc.window)
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				request := decodeRangeRequest(t, r)
				step := time.Duration(request.Step) * time.Second
				requestStart := time.Unix(request.Timestamp-request.Window, 0).UTC()
				if requestStart.Before(start) {
					t.Errorf("request starts at %s, before requested start %s", requestStart, start)
				}
				values := make([][2]any, 0)
				for at := requestStart; !at.After(time.Unix(request.Timestamp, 0)); at = at.Add(step) {
					if tc.occurrence.After(at.Add(-step)) && !tc.occurrence.After(at) {
						values = append(values, [2]any{at.Unix(), tc.value})
					}
				}
				_ = json.NewEncoder(w).Encode([]any{map[string]any{
					"metric": map[string]string{
						"exception_type": "TypeError", "service_name": "web", "span_name": "View",
						"span_kind": "SPAN_KIND_SERVER", "env": "prod",
					},
					"values": values,
				}})
			}))
			defer srv.Close()

			seen, err := fetchExceptionSeen(t.Context(), srv.Client(), exceptionTestConfig(srv.URL), exceptionSeenQuery{Start: start, End: end})
			if err != nil {
				t.Fatal(err)
			}
			wantRequests := 1
			if tc.window%exceptionSeenStep(tc.window) != 0 {
				wantRequests = 2
			}
			if requests != wantRequests {
				t.Errorf("requests = %d, want %d", requests, wantRequests)
			}
			_, gotSeen := seen[key]
			if gotSeen != tc.wantSeen {
				t.Fatalf("occurrence at %s: seen = %#v, want present = %t", tc.occurrence, seen, tc.wantSeen)
			}
		})
	}
}
