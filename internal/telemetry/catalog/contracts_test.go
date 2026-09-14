package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

func TestDecodeContractsRejectsUnsafeOrAmbiguousDescriptors(t *testing.T) {
	for _, body := range []string{
		`[{"datasource":"prod","source":"logs","schema_version":1},{"datasource":"prod","source":"logs","schema_version":1}]`,
		`[{"datasource":"","source":"logs","schema_version":1}]`,
		`[{"datasource":"prod","source":"logs","schema_version":0}]`,
		`[{"datasource":"prod","source":"model","schema_version":1}]`,
		`[] []`,
	} {
		if _, err := DecodeContracts([]byte(body)); err == nil {
			t.Fatalf("DecodeContracts(%s) succeeded", body)
		}
	}
}

func TestContractsLookupUsesConfiguredBoundary(t *testing.T) {
	contracts, err := DecodeContracts([]byte(`[{"datasource":"prod","source":"logs","index":"physical_index: app","schema_version":1,"fields":{"ServiceName":"string"},"backend_limits":{"no_hidden_sampling":true,"max_rows":101}}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.Lookup("prod", "logs", "physical_index:app", 1); !ok {
		t.Fatal("configured descriptor was not found")
	}
	for _, bad := range [][4]any{{"other", "logs", "physical_index:app", 1}, {"prod", "traces", "", 1}, {"prod", "logs", "physical_index:other", 1}, {"prod", "logs", "physical_index:app", 2}} {
		if _, ok := contracts.Lookup(bad[0].(string), bad[1].(string), bad[2].(string), bad[3].(int)); ok {
			t.Fatalf("unexpected descriptor lookup hit: %#v", bad)
		}
	}
}

func TestFetchContractsUsesDatasourceIdentityAndAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/datasources/ds-1/api-source-contracts/" || r.URL.Query().Get("index") != "physical_index:app" || r.URL.Query().Get("schema_version") != "1" {
			t.Fatalf("unexpected source-contract request: %s", r.URL.String())
		}
		if r.Header.Get(constants.HeaderXLast9APIToken) != "Bearer token" {
			t.Fatalf("missing access token")
		}
		_, _ = w.Write([]byte(`[{"datasource":"prod","source":"logs","index":"physical_index:app","schema_version":1,"backend_limits":{"no_hidden_sampling":true,"max_rows":5001},"execution":{"record_unit":"log_record","parser_stages":[]}}]`))
	}))
	defer server.Close()
	cfg := models.Config{
		APIBaseURL: server.URL, Datasources: []models.DatasourceInfo{{ID: "ds-1", Name: "prod"}},
		TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)},
	}
	contracts, err := FetchContracts(context.Background(), server.Client(), cfg, "prod", "physical_index:app")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.Lookup("prod", "logs", "physical_index:app", 1); !ok {
		t.Fatal("API contract was not indexed")
	}
}
