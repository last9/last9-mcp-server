package prom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/apitest"
	"last9-mcp/internal/models"
)

func TestDiscoverServiceNames(t *testing.T)         { testDiscoverServiceNames(t, false) }
func TestDiscoverServiceNamesInMemory(t *testing.T) { testDiscoverServiceNames(t, true) }
func testDiscoverServiceNames(t *testing.T, memory bool) {
	for _, tt := range []struct {
		name, body string
		status     int
		bad        bool
	}{
		{"success", `[{"metric":{"service_name":"checkout"},"value":[1,"2"]}]`, 200, false},
		{"empty", `[]`, 200, false},
		{"failed", `[]`, 503, true},
		{"null", `null`, 200, true},
		{"no body", ``, 200, true},
		{"partial", `{"status":"success","data":[],"partial":true}`, 200, true},
		{"missing label", `[{"metric":{"env":"prod"},"value":[1,"2"]}]`, 200, true},
		{"trailing", `[] []`, 200, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := QueryInstantInput{Datasource: Datasource{ReadURL: "https://selected.example", Username: "selected", Password: "test"}, Query: `sum by (service_name) (physical_index_service_count{destination="logs",env="prod"})`, Timestamp: 1700003600}
			base, client := apitest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got QueryInstantInput
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || r.URL.Path != "/prom_query_instant" || got != input {
					t.Errorf("request: %s %s %+v", r.Method, r.URL.Path, got)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}), memory)
			cfg := models.Config{APIBaseURL: base, TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
			names, err := DiscoverServiceNames(context.Background(), last9api.NewClient(client, cfg), input)
			if (err != nil) != tt.bad {
				t.Fatalf("names=%v err=%v", names, err)
			}
			if tt.name == "success" && (len(names) != 1 || names[0] != "checkout") {
				t.Fatalf("names=%v", names)
			}
		})
	}
}
