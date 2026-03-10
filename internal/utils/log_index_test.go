package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func TestResolveLogIndexDashboardParam(t *testing.T) {
	tests := []struct {
		name     string
		index    string
		endpoint string
		response string
		want     string
	}{
		{
			name:     "physical index resolves by name",
			index:    "physical_index:payments",
			endpoint: "/logs_settings/physical_indexes",
			response: `{"properties":[{"id":"idx-123","name":"payments"}]}`,
			want:     "physical:idx-123",
		},
		{
			name:     "rehydration index resolves by block name",
			index:    "rehydration_index:block-a",
			endpoint: "/logs_settings/rehydration",
			response: `{"properties":[{"id":"rehyd-456","block_name":"block-a"}]}`,
			want:     "rehydration:rehyd-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.endpoint {
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				if got := r.URL.Query().Get("region"); got != "us-east-1" {
					t.Fatalf("unexpected region %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			cfg := models.Config{
				APIBaseURL: server.URL,
				Region:     "us-east-1",
				TokenManager: &auth.TokenManager{
					AccessToken: "test-token",
					ExpiresAt:   time.Now().Add(24 * time.Hour),
				},
			}

			got, err := ResolveLogIndexDashboardParam(context.Background(), server.Client(), cfg, tt.index)
			if err != nil {
				t.Fatalf("ResolveLogIndexDashboardParam() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveLogIndexDashboardParam() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeLogIndexRejectsUnsupportedValues(t *testing.T) {
	if _, err := NormalizeLogIndex("physical:idx-123"); err == nil {
		t.Fatal("NormalizeLogIndex() expected an error for unsupported index format")
	}
}
