package traces

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryTraceAttributeDeviationsDecodesRankedValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(deviationEndpointFixture(t))
	}))
	defer server.Close()

	got, err := QueryTraceAttributeDeviations(context.Background(), server.Client(), tracesTestConfig(server.URL), GetTraceAttributeDeviationsArgs{
		ComparisonMode: "time", ServiceName: "checkout", Environment: "production",
		StartTimeISO: "2026-07-15T08:15:00Z", EndTimeISO: "2026-07-15T08:30:00Z",
		BaselineStartISO: "2026-07-15T08:00:00Z", BaselineEndISO: "2026-07-15T08:15:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Deviations) != 1 {
		t.Fatalf("deviations = %+v", got.Deviations)
	}
	d := got.Deviations[0]
	if d.Attribute != "service.version" || d.Value != "v2" || d.PercentagePointDelta != 40 || d.Rank != 1 || d.TargetCount != 60 || d.ControlCount != 20 {
		t.Fatalf("deviation = %+v", d)
	}
	if got.Warnings == nil {
		t.Fatal("warnings must decode to an empty list, not nil")
	}
}

func TestQueryTraceAttributeDeviationsValidatesBeforeCalling(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	defer server.Close()

	_, err := QueryTraceAttributeDeviations(context.Background(), server.Client(), tracesTestConfig(server.URL), GetTraceAttributeDeviationsArgs{
		ComparisonMode: "time", ServiceName: "checkout",
	})
	if err == nil || calls != 0 {
		t.Fatalf("err = %v, calls = %d; want a validation error before any request", err, calls)
	}
}
