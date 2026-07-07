package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMakeLogsJSONQueryAPI_400WrapsSelfHealingHint(t *testing.T) {
	const bodyText = `{"error":"invalid pipeline: unknown stage type"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(bodyText))
	}))
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{{"type": "filter"}}

	resp, err := MakeLogsJSONQueryAPI(context.Background(), srv.Client(), cfg, pipeline, 0, 60000, 100, "")
	if resp != nil {
		t.Fatalf("expected nil response on 400, got %#v", resp)
	}
	if err == nil {
		t.Fatal("expected an error for a 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "get_log_attributes_for_pipeline") {
		t.Errorf("error missing self-healing hint: %v", err)
	}
	if !strings.Contains(err.Error(), bodyText) {
		t.Errorf("error missing original body: %v", err)
	}
}

func TestMakeLogsJSONQueryAPI_500NoHint(t *testing.T) {
	const bodyText = `{"error":"internal server error"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(bodyText))
	}))
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{{"type": "filter"}}

	resp, err := MakeLogsJSONQueryAPI(context.Background(), srv.Client(), cfg, pipeline, 0, 60000, 100, "")
	if err != nil {
		t.Fatalf("expected no error from MakeLogsJSONQueryAPI for a 500 (caller handles non-400 statuses), got: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestMakeLogsJSONQueryAPI_200NoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"resultType":"streams","result":[]}}`))
	}))
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{{"type": "filter"}}

	resp, err := MakeLogsJSONQueryAPI(context.Background(), srv.Client(), cfg, pipeline, 0, 60000, 100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
