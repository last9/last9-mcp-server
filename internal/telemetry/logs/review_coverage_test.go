package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReviewCoverageWirePrecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		start    string
		end      string
		complete bool
	}{
		{"whole_seconds", "2026-01-01T00:00:00Z", "2026-01-01T00:00:10Z", true},
		{"milliseconds", "2026-01-01T00:00:00.500Z", "2026-01-01T00:00:10.500Z", false},
		{"sub_milliseconds", "2026-01-01T00:00:00.500001Z", "2026-01-01T00:00:10.500001Z", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("start") != "1767225600" || r.URL.Query().Get("end") != "1767225610" {
					t.Errorf("unexpected wire bounds %v", r.URL.Query())
				}
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			}))
			defer srv.Close()
			got, _, err := NewGetServiceLogsHandler(srv.Client(), testLogsConfig(srv.URL))(context.Background(), nil, GetServiceLogsArgs{ServiceName: "synthetic", StartTimeISO: tc.start, EndTimeISO: tc.end, Limit: 20})
			if err != nil {
				t.Fatal(err)
			}
			coverage, _ := got.Meta["last9/coverage"].(map[string]any)
			if tc.complete {
				if coverage["status"] != "complete" {
					t.Fatalf("positive control: %v", coverage)
				}
			} else if coverage["status"] == "complete" {
				t.Fatalf("claims complete for unqueried fractional-second tail: %v", coverage)
			}
		})
	}
}
