package alerting

import (
	"context"
	"errors"
	"net/http/httptest"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/utils"
	"net/http"
	"testing"
)

func TestAlertIntelErrorClassStrings(t *testing.T) {
	for class, want := range map[alertIntelErrorClass]string{
		classCoverageMiss:   "coverage_miss",
		classPermissions:    "permissions",
		classDuplicateName:  "duplicate_name",
		classUpstream:       "upstream",
		classInvalidRequest: "invalid_request",
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
		{"other 400", http.StatusBadRequest, `{"message":"invalid payload"}`, classInvalidRequest},
		{"422", http.StatusUnprocessableEntity, `{"message":"invalid payload"}`, classInvalidRequest},
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

func TestDescribeAlertChartUpstreamErrorsAreClassified(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       alertIntelErrorClass
	}{
		{"coverage miss chart", http.StatusBadRequest, `{"message":"unknown chart key: latency_p95"}`, classCoverageMiss},
		{"coverage miss signal", http.StatusBadRequest, `{"message":"unknown signal key: p95"}`, classCoverageMiss},
		{"invalid request 400", http.StatusBadRequest, `{"message":"invalid payload"}`, classInvalidRequest},
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
			_, err := alerts.DescribeAlertChart(context.Background(), last9api.NewClient(server.Client(), testAlertMutationConfig(server.URL)), alerts.IntelligenceRequest{})
			var apiErr *utils.UpstreamHTTPError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected UpstreamHTTPError, got %v", err)
			}
			if apiErr.StatusCode != tt.statusCode {
				t.Fatalf("status = %d, want %d", apiErr.StatusCode, tt.statusCode)
			}
			if got := classifyAlertIntelligenceError(apiErr.StatusCode, apiErr.Body); got != tt.want {
				t.Fatalf("class = %s, want %s", got, tt.want)
			}
		})
	}
}
