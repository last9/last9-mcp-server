package traces

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReviewExceptionSeenDoesNotReadBeforeRequestedWindow(t *testing.T) {
	start := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		window time.Duration
		values string
		want   exceptionSeen
	}{
		{
			name:   "divisible window excludes start and invalid samples but includes end",
			window: 60 * time.Minute,
			values: fmt.Sprintf(`[[%d,"1"],[%d,"NaN"],[%d,"2"]]`, start.Unix(), start.Add(time.Minute).Unix(), start.Add(60*time.Minute).Unix()),
			want:   exceptionSeen{First: start.Add(60 * time.Minute), Last: start.Add(60 * time.Minute)},
		},
		{name: "non-divisible window excludes samples before start", window: 61 * time.Minute, values: `[]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			end := start.Add(tc.window)
			var request promRangeRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
				}
				body := fmt.Sprintf(`[{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"values":%s}]`, tc.values)
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()

			seen, err := fetchExceptionSeen(t.Context(), srv.Client(), exceptionTestConfig(srv.URL), exceptionSeenQuery{Start: start, End: end})
			if err != nil {
				t.Fatal(err)
			}
			step := time.Duration(request.Step) * time.Second
			wantWindow := (tc.window + step - 1) / step * step
			if request.Window != int64(wantWindow/time.Second) {
				t.Errorf("window = %d, want %d", request.Window, int64(wantWindow/time.Second))
			}
			got, found := seen[exceptionSeriesKey(map[string]string{"exception_type": "TypeError", "service_name": "web", "span_name": "View", "span_kind": "SPAN_KIND_SERVER", "env": "prod"})]
			if got != tc.want || found != !tc.want.First.IsZero() {
				t.Fatalf("seen = %#v, found %v; want %#v, found %v", got, found, tc.want, !tc.want.First.IsZero())
			}
		})
	}
}

func TestReviewExceptionSeenCoversFinalPartialStep(t *testing.T) {
	start := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	for _, window := range []time.Duration{60 * time.Minute, 61 * time.Minute} {
		t.Run(window.String(), func(t *testing.T) {
			end := start.Add(window)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request := decodeRangeRequest(t, r)
				requestStart := time.Unix(request.Timestamp-request.Window, 0).UTC()
				values := make([][2]any, 0)
				firstInWindow := true
				for at := requestStart; !at.After(end); at = at.Add(time.Duration(request.Step) * time.Second) {
					value := "0"
					switch {
					case !at.After(start):
						value = "1"
					case firstInWindow:
						value = "+Inf"
						firstInWindow = false
					case !at.Before(end):
						value = "1"
					}
					values = append(values, [2]any{at.Unix(), value})
				}
				body, err := json.Marshal([]any{map[string]any{
					"metric": map[string]string{"exception_type": "TypeError"},
					"values": values,
				}})
				if err != nil {
					t.Errorf("marshal response: %v", err)
					return
				}
				_, _ = w.Write(body)
			}))
			defer srv.Close()

			seen, err := fetchExceptionSeen(t.Context(), srv.Client(), exceptionTestConfig(srv.URL), exceptionSeenQuery{Start: start, End: end})
			if err != nil {
				t.Fatal(err)
			}
			got := seen[exceptionSeriesKey(map[string]string{"exception_type": "TypeError"})]
			if got.First != end || got.Last != end {
				t.Fatalf("seen = %#v, want occurrence at final evaluation %s", got, end)
			}
		})
	}
}

func TestReviewExceptionSeenEndAlignmentDoesNotAdmitPreWindowEvidence(t *testing.T) {
	start := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	end := start.Add(61 * time.Minute)
	key := exceptionSeriesKey(map[string]string{
		"exception_type": "TypeError", "service_name": "web", "span_name": "View",
		"span_kind": "SPAN_KIND_SERVER", "env": "prod",
	})
	for _, tc := range []struct {
		name       string
		occurrence time.Time
		wantSeen   bool
	}{
		{name: "before requested window", occurrence: start.Add(-30 * time.Second)},
		{name: "inside final partial interval", occurrence: end.Add(-30 * time.Second), wantSeen: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request := decodeRangeRequest(t, r)
				step := time.Duration(request.Step) * time.Second
				requestStart := time.Unix(request.Timestamp-request.Window, 0).UTC()
				values := make([][2]any, 0)
				for at := requestStart; !at.After(end); at = at.Add(step) {
					if tc.occurrence.After(at.Add(-step)) && !tc.occurrence.After(at) {
						values = append(values, [2]any{at.Unix(), "1"})
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
			_, gotSeen := seen[key]
			if gotSeen != tc.wantSeen {
				t.Fatalf("occurrence at %s: seen = %#v, want present = %t", tc.occurrence, seen, tc.wantSeen)
			}
		})
	}
}
