package grafana

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const sampleDatasources = `[
  {"id": 1, "uid": "ds1", "name": "Prometheus", "type": "prometheus",
   "url": "http://prometheus:9090", "access": "proxy", "isDefault": true,
   "basicAuthUser": "admin", "basicAuthPassword": "secret", "password": "secret",
   "secureJsonFields": {"tlsClientSecret": true}}
]`

func TestListDatasourcesHandler(t *testing.T) {
	var gotPath string
	srv := newRecordingServer("/api/datasources", sampleDatasources, new(string), &gotPath)
	defer srv.Close()

	result, _, err := NewListDatasourcesHandler(srv.Client(), testGrafanaConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, ListDatasourcesArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text

	if gotPath != "/api/datasources" {
		t.Fatalf("datasources request path = %q", gotPath)
	}

	var dss []Datasource
	if err := json.Unmarshal([]byte(text), &dss); err != nil {
		t.Fatalf("datasources result not valid JSON: %v", err)
	}
	if len(dss) != 1 || dss[0].UID != "ds1" || dss[0].Name != "Prometheus" || !dss[0].IsDefault {
		t.Fatalf("datasources = %+v", dss)
	}

	// Credential fields must never reach the model.
	for _, deny := range []string{"secret", "basicAuthPassword", "secureJsonFields", "password"} {
		if strings.Contains(text, deny) {
			t.Errorf("datasources result leaked %q: %s", deny, text)
		}
	}
}
