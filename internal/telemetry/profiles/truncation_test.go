package profiles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Returned stack rows are a bounded subset, so derived tools must disclose it.
func TestTruncatedRankingsDiscloseCap(t *testing.T) {
	for _, kind := range []string{"flamegraph", "top", "summary"} {
		t.Run(kind, func(t *testing.T) {
			// 1000 heavier distinct stacks precede 1001 lighter stacks of the same hot leaf.
			// Server ordering is sample-descending; the omitted leaf contributes 9009/19009 samples.
			rows := make([]map[string]any, 0, 2001)
			for i := 0; i < 1000; i++ {
				rows = append(rows, map[string]any{
					"StackHash": fmt.Sprint(i),
					"Frames":    fmt.Sprintf("leaf%d%smain", i, FrameDelimiter),
					"samples":   10,
				})
			}
			for i := 0; i < 1001; i++ {
				rows = append(rows, map[string]any{
					"StackHash": fmt.Sprint(1000 + i),
					"Frames":    fmt.Sprintf("hottest%scaller%d", FrameDelimiter, i),
					"samples":   9,
				})
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Pipeline []map[string]any `json:"pipeline"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				limit := int(req.Pipeline[len(req.Pipeline)-1]["limit"].(float64))
				if limit > len(rows) {
					limit = len(rows)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(dataframeBody(rows[:limit]...))
			}))
			defer srv.Close()

			cfg := testCfg(srv.URL)
			var res *mcp.CallToolResult
			var err error
			switch kind {
			case "flamegraph":
				res, _, err = NewGetFlamegraphHandler(srv.Client(), cfg)(context.Background(), nil, GetFlamegraphArgs{Service: "api"})
			case "top":
				res, _, err = NewGetTopFunctionsHandler(srv.Client(), cfg)(context.Background(), nil, GetTopFunctionsArgs{Service: "api"})
			case "summary":
				res, _, err = NewGetProfileSummaryHandler(srv.Client(), cfg)(context.Background(), nil, GetProfileSummaryArgs{Service: "api"})
			}
			if err != nil || res.IsError {
				t.Fatalf("%+v %v", res, err)
			}

			var got map[string]any
			if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &got); err != nil {
				t.Fatal(err)
			}
			if got["total_samples"] != float64(10000) {
				t.Fatalf("fixture no longer capped: %v", got["total_samples"])
			}
			if got["truncated"] != true {
				t.Fatalf("bounded 1000/2001 stacks reported without truncation: total_samples=%v truncated=%v; full fixture total=19009",
					got["total_samples"], got["truncated"])
			}
			if kind == "summary" {
				summary, _ := got["summary"].(string)
				if !strings.Contains(summary, "truncated") {
					t.Fatalf("summary text must disclose truncation: %q", summary)
				}
			}
		})
	}
}
