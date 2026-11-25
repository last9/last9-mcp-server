package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDatabaseQueriesCallingService(t *testing.T) {
	const firstService = `api."a\b"`
	const secondService = "api-worker"
	for _, tc := range []struct {
		name    string
		service string
		want    QueryPattern
	}{
		{"exact service with special characters", firstService, QueryPattern{"SELECT", 2, 3, 5, 10}},
		{"second caller", secondService, QueryPattern{"SELECT", 7, 17, 19, 20}},
		{"all callers", "", QueryPattern{"SELECT", 9, 10, 12, 18}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Query string }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				q := body.Query
				calls.Add(1)
				for _, matcher := range []string{`db_system="postgresql"`, `net_peer_name="db.example"`, `env=~`} {
					if !strings.Contains(q, matcher) {
						t.Errorf("missing %s in %s", matcher, q)
					}
				}
				values := [5]int{9, 10, 12, 9, 50}
				switch {
				case strings.Contains(q, "service_name="+strconv.Quote(firstService)):
					values = [5]int{2, 3, 5, 1, 10}
				case strings.Contains(q, "service_name="+strconv.Quote(secondService)):
					values = [5]int{7, 17, 19, 8, 40}
				case strings.Contains(q, "service_name"):
					t.Errorf("unexpected service matcher in %s", q)
				}
				value := values[4]
				switch {
				case strings.Contains(q, " / "):
					value = values[0]
				case strings.Contains(q, `quantile="p50"`):
					value = values[1]
				case strings.Contains(q, `quantile="p95"`):
					value = values[2]
				case strings.Contains(q, `status_code="STATUS_CODE_ERROR"`):
					value = values[3]
				}
				fmt.Fprint(w, opsSumRows(opsSumRow(map[string]string{"span_name": "SELECT"}, strconv.Itoa(value))))
			}))
			defer srv.Close()
			result, _, err := NewGetDatabaseQueriesHandler(srv.Client(), testDBConfig(srv.URL))(context.Background(), nil, GetDatabaseQueriesArgs{DBSystem: "postgresql", Host: "db.example", Env: "prod", ServiceName: tc.service, LookbackMinutes: 60})
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				ServiceName *string `json:"service_name"`
				Queries     []QueryPattern
			}
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &response); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 5 || !reflect.DeepEqual(response.Queries, []QueryPattern{tc.want}) {
				t.Errorf("got %d queries and rows %+v, want five queries and row %+v", calls.Load(), response.Queries, tc.want)
			}
			if tc.service == "" && response.ServiceName != nil || tc.service != "" && (response.ServiceName == nil || *response.ServiceName != tc.service) {
				t.Errorf("unexpected service_name in %s", result.Content[0].(*mcp.TextContent).Text)
			}
		})
	}
}
