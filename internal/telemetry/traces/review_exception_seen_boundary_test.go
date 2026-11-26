package traces

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReviewExceptionSeenDoesNotReadBeforeRequestedWindow(t *testing.T) {
	start := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	end := start.Add(61 * time.Minute)
	var request promRangeRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		body := `[]`
		if request.Window > 61*60 {
			body = `[{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"values":[[1768903260,"1"]]}]`
		}
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	seen, err := fetchExceptionSeen(t.Context(), srv.Client(), exceptionTestConfig(srv.URL), exceptionSeenQuery{Start: start, End: end})
	if err != nil {
		t.Fatal(err)
	}
	if request.Window != 61*60 {
		t.Errorf("window = %d, want %d", request.Window, 61*60)
	}
	if len(seen) != 0 {
		t.Fatalf("pre-window evidence produced timestamps: %#v", seen)
	}
}
