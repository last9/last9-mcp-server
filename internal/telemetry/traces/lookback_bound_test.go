package traces

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const outOfRangeLookbackMinutes = 10000000

func TestTraceToolsRejectOutOfRangeLookbackWithoutUpstreamCall(t *testing.T) {
	tests := []struct {
		name string
		call func(client *http.Client, url string) error
	}{
		{
			name: "get_traces",
			call: func(client *http.Client, url string) error {
				_, _, err := NewGetTracesHandler(client, testChunkTracesConfig(url))(context.Background(), &mcp.CallToolRequest{},
					GetTracesArgs{
						TracejsonQuery:  []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}},
						LookbackMinutes: outOfRangeLookbackMinutes,
					})
				return err
			},
		},
		{
			name: "get_service_traces",
			call: func(client *http.Client, url string) error {
				_, _, err := GetServiceTracesHandler(client, testChunkTracesConfig(url))(context.Background(), &mcp.CallToolRequest{},
					GetServiceTracesArgs{ServiceName: "checkout", LookbackMinutes: outOfRangeLookbackMinutes})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
			}))
			defer server.Close()

			err := tt.call(server.Client(), server.URL)
			if err == nil || !strings.Contains(err.Error(), "between 1 and 527040 (366 days)") {
				t.Fatalf("want an error naming the allowed range, got %v", err)
			}
			if n := calls.Load(); n != 0 {
				t.Fatalf("want no upstream calls, got %d", n)
			}
		})
	}
}
