package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchAlertsMonitor_ErrorBodyIsSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad","auth":"Bearer abc123","url":"https://internal.example/x","token=sekret"}`))
	}))
	defer srv.Close()

	_, err := FetchAlertsMonitor(context.Background(), srv.Client(), newTestNotificationChannelsConfig(srv.URL), AlertsMonitorQuery{Timestamp: 1, Window: 60})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	for _, secret := range []string{"abc123", "internal.example", "sekret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}
