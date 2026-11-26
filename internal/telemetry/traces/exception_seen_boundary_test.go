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
		{
			name:   "non-divisible window does not round request before start",
			window: 61 * time.Minute,
			values: `[]`,
		},
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
				if request.Window > int64(tc.window/time.Second) {
					body = `[{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"values":[[1768903260,"1"]]}]`
				}
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()

			seen, err := fetchExceptionSeen(t.Context(), srv.Client(), exceptionTestConfig(srv.URL), exceptionSeenQuery{Start: start, End: end})
			if err != nil {
				t.Fatal(err)
			}
			if request.Window != int64(tc.window/time.Second) {
				t.Errorf("window = %d, want %d", request.Window, int64(tc.window/time.Second))
			}
			got, found := seen[exceptionSeriesKey(map[string]string{"exception_type": "TypeError", "service_name": "web", "span_name": "View", "span_kind": "SPAN_KIND_SERVER", "env": "prod"})]
			if got != tc.want || found != !tc.want.First.IsZero() {
				t.Fatalf("seen = %#v, found %v; want %#v, found %v", got, found, tc.want, !tc.want.First.IsZero())
			}
		})
	}
}
