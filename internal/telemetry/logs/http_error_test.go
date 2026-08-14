package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsRelays400BodyAndDrains502(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
		forbid     string
	}{
		{
			name:       "400 includes parse body",
			status:     http.StatusBadRequest,
			body:       `{"error":"invalid json pipeline: unknown stage"}`,
			wantSubstr: "unknown stage",
		},
		{
			name:       "502 omits body",
			status:     http.StatusBadGateway,
			body:       `{"error":"gateway SECRET"}`,
			wantSubstr: "HTTP 502",
			forbid:     "SECRET",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery: []map[string]interface{}{
					{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}},
				},
				LookbackMinutes: 5,
			})
			if err == nil {
				t.Fatal("expected tool error")
			}
			got := err.Error()
			if !strings.Contains(got, tt.wantSubstr) {
				t.Fatalf("error %q missing %q", got, tt.wantSubstr)
			}
			if tt.forbid != "" && strings.Contains(got, tt.forbid) {
				t.Fatalf("error leaked %q: %s", tt.forbid, got)
			}
		})
	}
}
