package dashboards

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"
)

func TestDoJSONRequest_GETSuccess(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get(constants.HeaderXLast9APIToken)
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"d1","name":"CPU"}]`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	body, status, err := doJSONRequest(
		context.Background(),
		srv.Client(),
		cfg,
		http.MethodGet,
		srv.URL+constants.EndpointDashboards,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status %d body %s", status, body)
	}
	if !strings.HasPrefix(gotAuth, constants.BearerPrefix) {
		t.Fatalf("auth header %q", gotAuth)
	}
}
