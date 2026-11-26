package traces

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExceptionSeenKeepsBucketsInsideRequestedWindow(t *testing.T) {
	wholeSecondStart := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	key := exceptionSeriesKey(map[string]string{
		"exception_type": "TypeError", "service_name": "web", "span_name": "View",
		"span_kind": "SPAN_KIND_SERVER", "env": "prod",
	})
	for _, tc := range []struct {
		name       string
		start      time.Time
		window     time.Duration
		occurrence time.Time
		value      string
		wantSeen   bool
		wantTail   time.Duration
	}{
		{name: "divisible window excludes start", start: wholeSecondStart, window: 60 * time.Minute, occurrence: wholeSecondStart},
		{name: "divisible window includes end", start: wholeSecondStart, window: 60 * time.Minute, occurrence: wholeSecondStart.Add(60 * time.Minute), value: "1", wantSeen: true},
		{name: "non-divisible window excludes pre-window evidence", start: wholeSecondStart, window: 61 * time.Minute, occurrence: wholeSecondStart.Add(-30 * time.Second), value: "1"},
		{name: "non-divisible window includes final tail", start: wholeSecondStart, window: 61 * time.Minute, occurrence: wholeSecondStart.Add(60*time.Minute + 30*time.Second), value: "1", wantSeen: true},
		{name: "invalid sample is not an occurrence", start: wholeSecondStart, window: 61 * time.Minute, occurrence: wholeSecondStart.Add(30 * time.Minute), value: "+Inf"},
		{name: "fractional window excludes pre-window evidence", start: wholeSecondStart.Add(900 * time.Millisecond), window: 60 * time.Minute, occurrence: wholeSecondStart.Add(700 * time.Millisecond), value: "1"},
		{name: "fractional window includes first bucket", start: wholeSecondStart.Add(900 * time.Millisecond), window: 60 * time.Minute, occurrence: wholeSecondStart.Add(30*time.Second + 900*time.Millisecond), value: "1", wantSeen: true},
		{name: "fractional window includes last full bucket", start: wholeSecondStart.Add(900 * time.Millisecond), window: 60 * time.Minute, occurrence: wholeSecondStart.Add(59*time.Minute + 30*time.Second + 900*time.Millisecond), value: "1", wantSeen: true},
		{name: "fractional window includes end fraction", start: wholeSecondStart.Add(900 * time.Millisecond), window: 60 * time.Minute, occurrence: wholeSecondStart.Add(60*time.Minute + 700*time.Millisecond), value: "1", wantSeen: true},
		{name: "subsecond tail includes first fraction", start: wholeSecondStart, window: 60*time.Minute + 500*time.Millisecond, occurrence: wholeSecondStart.Add(60*time.Minute + 100*time.Millisecond), value: "1", wantSeen: true, wantTail: 500 * time.Millisecond},
		{name: "submillisecond tail is below occurrence precision", start: wholeSecondStart, window: 60*time.Minute + 500*time.Microsecond, occurrence: wholeSecondStart.Add(60*time.Minute + 100*time.Microsecond)},
		{name: "millisecond tail is at occurrence precision", start: wholeSecondStart, window: 60*time.Minute + time.Millisecond, occurrence: wholeSecondStart.Add(60*time.Minute + 500*time.Microsecond), value: "1", wantSeen: true, wantTail: time.Millisecond},
		{name: "fractional millisecond tail uses supported precision", start: wholeSecondStart, window: 60*time.Minute + 1500*time.Microsecond, occurrence: wholeSecondStart.Add(60*time.Minute + 500*time.Microsecond), value: "1", wantSeen: true, wantTail: time.Millisecond},
		{name: "minute tail includes first fraction", start: wholeSecondStart, window: 61*time.Minute + 500*time.Millisecond, occurrence: wholeSecondStart.Add(60*time.Minute + 100*time.Millisecond), value: "1", wantSeen: true, wantTail: 60*time.Second + 500*time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := tc.start
			end := start.Add(tc.window)
			normalizedStart := start.Truncate(time.Millisecond)
			if normalizedStart.Before(start) {
				normalizedStart = normalizedStart.Add(time.Millisecond)
			}
			normalizedEnd := end.Truncate(time.Millisecond)
			normalizedWindow := normalizedEnd.Sub(normalizedStart)
			tail := tc.wantTail
			if tail == 0 {
				tail = normalizedWindow % exceptionSeenStep(normalizedWindow)
			}
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				request := decodeRangeRequest(t, r)
				step := time.Duration(request.Step) * time.Second
				requestStart := timeFromUnixSeconds(request.Timestamp - request.Window)
				if requestStart.Before(start) {
					t.Errorf("request starts at %s, before requested start %s", requestStart, start)
				}
				values := make([][2]any, 0)
				requestEnd := timeFromUnixSeconds(request.Timestamp)
				if request.Window == 0 {
					wantSelector := "[" + prometheusDuration(tail) + "]"
					if !strings.Contains(request.Query, wantSelector) {
						t.Errorf("tail query = %q, want selector %q", request.Query, wantSelector)
					}
					if tc.occurrence.After(requestEnd.Add(-tail)) && !tc.occurrence.After(requestEnd) {
						values = append(values, [2]any{unixSeconds(requestEnd), tc.value})
					}
				} else {
					for at := requestStart; !at.After(requestEnd); at = at.Add(step) {
						if tc.occurrence.After(at.Add(-step)) && !tc.occurrence.After(at) {
							values = append(values, [2]any{unixSeconds(at), tc.value})
						}
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
			if normalizedWindow%exceptionSeenStep(normalizedWindow) != 0 {
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
