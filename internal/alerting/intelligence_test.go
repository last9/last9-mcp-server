package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"
)

func TestCallAlertIntelligenceHappyPath(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != constants.EndpointAlertIntelligence {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		if got := r.Header.Get(constants.HeaderXLast9APIToken); got != constants.BearerPrefix+"test-token" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get(constants.HeaderContentType); got != constants.HeaderContentTypeJSON {
			t.Errorf("content-type = %q", got)
		}
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"rule-1","signals":[{"key":"p95_latency","eval_query":"expr","unit":"ms","query_kind":"metric"}],"group_id":"grp-1","kpi_id":"kpi-1"}`))
	}))
	defer server.Close()

	payload, err := json.Marshal(AlertIntelligenceRequest{Operation: AlertIntelCreateFromChart, ChartKey: "chart-1", SignalKey: "p95_latency"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(gotBody, []byte(`"operation":"create_from_chart"`)) {
		t.Errorf("request body missing operation: %s", gotBody)
	}
	var resp AlertIntelligenceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != "rule-1" || resp.GroupID != "grp-1" || resp.KPIID != "kpi-1" || len(resp.Signals) != 1 || resp.Signals[0].Key != "p95_latency" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAlertIntelErrorClassStrings(t *testing.T) {
	for class, want := range map[alertIntelErrorClass]string{
		classCoverageMiss:  "coverage_miss",
		classPermissions:   "permissions",
		classDuplicateName: "duplicate_name",
		classUpstream:      "upstream",
	} {
		if got := class.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

func TestClassifyAlertIntelligenceError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       alertIntelErrorClass
	}{
		{"unknown chart key", http.StatusBadRequest, `{"message":"unknown chart key: latency_p95"}`, classCoverageMiss},
		{"unknown signal key", http.StatusBadRequest, `{"message":"unknown signal key: p95"}`, classCoverageMiss},
		{"invalid chart entity", http.StatusBadRequest, `{"message":"invalid chart entity"}`, classCoverageMiss},
		{"other 400", http.StatusBadRequest, `{"message":"invalid payload"}`, classUpstream},
		{"unauthorized", http.StatusUnauthorized, "unauthorized", classPermissions},
		{"forbidden", http.StatusForbidden, "Not allowed for viewer role", classPermissions},
		{"conflict", http.StatusConflict, "duplicate rule name", classDuplicateName},
		{"server error", http.StatusInternalServerError, "boom", classUpstream},
		{"bad gateway empty body", http.StatusBadGateway, "", classUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAlertIntelligenceError(tt.statusCode, tt.body); got != tt.want {
				t.Fatalf("class = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCallAlertIntelligenceClassifiesErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       alertIntelErrorClass
	}{
		{"coverage miss chart", http.StatusBadRequest, `{"message":"unknown chart key: latency_p95"}`, classCoverageMiss},
		{"coverage miss signal", http.StatusBadRequest, `{"message":"unknown signal key: p95"}`, classCoverageMiss},
		{"upstream 400", http.StatusBadRequest, `{"message":"invalid payload"}`, classUpstream},
		{"permissions", http.StatusForbidden, "Not allowed for viewer role", classPermissions},
		{"duplicate", http.StatusConflict, "duplicate rule name", classDuplicateName},
		{"upstream 500", http.StatusInternalServerError, "boom", classUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, tt.body, tt.statusCode)
			}))
			defer server.Close()
			_, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), []byte(`{"operation":"describe_chart"}`))
			var apiErr *AlertIntelHTTPError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected AlertIntelHTTPError, got %v", err)
			}
			if apiErr.Class != tt.want {
				t.Fatalf("class = %s, want %s", apiErr.Class, tt.want)
			}
			if apiErr.StatusCode != tt.statusCode {
				t.Fatalf("status = %d, want %d", apiErr.StatusCode, tt.statusCode)
			}
		})
	}
}

func TestCallAlertIntelligenceRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), int(maxAlertMutationResponseBytes)+1))
	}))
	defer server.Close()
	_, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), []byte(`{"operation":"describe_chart"}`))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected response cap error, got %v", err)
	}
	var apiErr *AlertIntelHTTPError
	if errors.As(err, &apiErr) {
		t.Fatalf("oversized response must not be classified/parsed as an API error: %+v", apiErr)
	}
}

func TestCallAlertIntelligenceRejectsEmptyPayload(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for _, payload := range [][]byte{nil, []byte("   ")} {
		_, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), payload)
		if err == nil || !strings.Contains(err.Error(), "payload is required") {
			t.Fatalf("expected empty payload rejection, got %v", err)
		}
	}
	if requests != 0 {
		t.Fatalf("server received %d requests for empty payloads", requests)
	}
}
