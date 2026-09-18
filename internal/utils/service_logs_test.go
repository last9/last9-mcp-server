package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMakeLogsJSONQueryAPI_400ReturnsResponse(t *testing.T) {
	const bodyText = `{"error":"invalid pipeline: unknown stage type","url":"https://internal.example/query"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(bodyText))
	}))
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{{"type": "filter"}}

	resp, err := MakeLogsJSONQueryAPI(context.Background(), srv.Client(), cfg, pipeline, 0, 60000, 100, "")
	if err != nil {
		t.Fatalf("400 must return the response so the caller can sanitize: %v", err)
	}
	if resp == nil {
		t.Fatal("expected 400 response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
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
