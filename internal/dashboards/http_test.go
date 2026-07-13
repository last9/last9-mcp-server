package dashboards

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"
)

func TestDoJSONRequest_4xxWithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, status, err := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodGet, srv.URL+constants.EndpointDashboards, nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status %d", status)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error %q", err)
	}
}

func TestDoJSONRequest_4xxEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, _, err := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodGet, srv.URL+constants.EndpointDashboards, nil)
	if err == nil {
		t.Fatal("expected error for 403 empty body")
	}
	if !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("expected status text fallback, got %q", err)
	}
}

func TestMapDashboardAPIError_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"readonly"}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, _, rawErr := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodDelete, srv.URL+constants.EndpointDashboards, nil)
	err := mapDashboardAPIError(rawErr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("expected readonly message, got %q", err)
	}
}

func TestMapDashboardAPIError_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, _, rawErr := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodGet, srv.URL+constants.EndpointDashboards, nil)
	err := mapDashboardAPIError(rawErr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found message, got %q", err)
	}
}

func TestMapSnapshotAPIError_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, _, rawErr := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodDelete, srv.URL+constants.EndpointDashboardSnapshots+"/snap-1", nil)
	err := mapSnapshotAPIError(rawErr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not permitted to access") {
		t.Fatalf("expected access-neutral message, got %q", err)
	}
}

func TestMapSnapshotAPIError_404Snapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, _, rawErr := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodGet, srv.URL+constants.EndpointDashboardSnapshots+"/snap-1", nil)
	err := mapSnapshotAPIError(rawErr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dashboard snapshot not found") {
		t.Fatalf("expected snapshot not found message, got %q", err)
	}
}

func TestMapSnapshotAPIError_404Dashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"error":"dashboard not found"}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, _, rawErr := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodGet, srv.URL+constants.EndpointDashboardSnapshots+"?dashboard_id=missing", nil)
	err := mapSnapshotAPIError(rawErr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dashboard not found") {
		t.Fatalf("expected dashboard not found message, got %q", err)
	}
	if strings.Contains(err.Error(), "dashboard snapshot not found") {
		t.Fatalf("should not mislabel as snapshot missing: %q", err)
	}
}

func TestDoJSONRequest_RejectsOversizedSuccessBody(t *testing.T) {
	prev := maxAPISuccessBodyBytes
	maxAPISuccessBodyBytes = 32
	t.Cleanup(func() { maxAPISuccessBodyBytes = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("a"), 64))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	_, _, err := doJSONRequest(context.Background(), srv.Client(), cfg, http.MethodGet, srv.URL+constants.EndpointDashboards, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized error, got %v", err)
	}
}

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
