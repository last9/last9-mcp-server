package apm

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetDatabaseServerMetricsHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		if strings.Contains(r.URL.Path, "prom_label_values") {
			// Probe request — return some pg_ metric names
			json.NewEncoder(w).Encode([]string{
				"pg_stat_activity_count",
				"pg_stat_database_blks_hit",
				"pg_settings_max_connections",
			})
		} else {
			// Instant query for metrics — return a single value
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"metric": map[string]string{},
					"value":  []any{1700000000, "42.5"},
				},
			})
		}
	}))
	defer server.Close()

	handler := NewGetDatabaseServerMetricsHandler(server.Client(), testDBConfig(server.URL))
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:     "postgresql",
		StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var response map[string]any
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	databases, ok := response["databases"].([]any)
	if !ok || len(databases) == 0 {
		t.Fatal("expected at least one database in response")
	}

	db := databases[0].(map[string]any)
	if db["db_system"] != "postgresql" {
		t.Errorf("expected db_system=postgresql, got %v", db["db_system"])
	}
	if db["available"] != true {
		t.Error("expected postgresql to be available")
	}
	if db["metrics"] == nil {
		t.Error("expected metrics map")
	}

	metrics := db["metrics"].(map[string]any)
	if metrics["active_connections"] == nil {
		t.Error("expected active_connections metric")
	}
}

func TestGetDatabaseServerMetricsHandler_NotAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return empty label values — no metrics found
		json.NewEncoder(w).Encode([]string{})
	}))
	defer server.Close()

	handler := NewGetDatabaseServerMetricsHandler(server.Client(), testDBConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:        "oracle",
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var response map[string]any
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	databases := response["databases"].([]any)
	db := databases[0].(map[string]any)
	if db["available"] != false {
		t.Error("expected oracle to be unavailable")
	}
}

func TestGetDatabaseServerMetricsHandler_KeepsKeysWithNoSeries(t *testing.T) {
	keyMetrics := dbExporterConfigs["oracle"].KeyMetrics
	answered := keyMetrics[0]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "prom_label_values") {
			json.NewEncoder(w).Encode([]string{"oracledb_up"})
			return
		}
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode instant query body: %v", err)
		}
		if body.Query != answered.Query {
			json.NewEncoder(w).Encode([]any{})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{{
			"metric": map[string]string{"oracledb_instance_name": "ORCL"},
			"value":  []any{1700000000, "7"},
		}})
	}))
	defer server.Close()

	handler := NewGetDatabaseServerMetricsHandler(server.Client(), testDBConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:        "oracle",
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var response struct {
		Databases []struct {
			Available      bool           `json:"available"`
			Metrics        map[string]any `json:"metrics"`
			MissingMetrics []string       `json:"missing_metrics"`
		} `json:"databases"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &response); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(response.Databases) != 1 {
		t.Fatalf("expected one database, got %d", len(response.Databases))
	}
	db := response.Databases[0]
	if !db.Available {
		t.Error("expected oracle to stay available when some key metrics have no series")
	}

	var wantMissing []string
	for _, km := range keyMetrics {
		value, present := db.Metrics[km.Name]
		if !present {
			t.Errorf("key %q missing from metrics", km.Name)
			continue
		}
		if km.Name == answered.Name {
			if value == nil {
				t.Errorf("key %q: expected a value, got null", km.Name)
			}
			continue
		}
		if value != nil {
			t.Errorf("key %q: expected null for a query with no series, got %v", km.Name, value)
		}
		wantMissing = append(wantMissing, km.Name)
	}
	sort.Strings(wantMissing)
	if strings.Join(db.MissingMetrics, ",") != strings.Join(wantMissing, ",") {
		t.Errorf("missing_metrics = %v, want %v", db.MissingMetrics, wantMissing)
	}
}

func TestOracleKeyMetricsUseOracledbExporterNames(t *testing.T) {
	want := map[string][]string{
		"active_sessions":          {"oracledb_sessions_usage"},
		"executions_per_sec":       {"oracledb_executions_total"},
		"user_commits_per_sec":     {"oracledb_user_commits_total"},
		"user_rollbacks_per_sec":   {"oracledb_user_rollbacks_total"},
		"parse_calls_per_sec":      {"oracledb_parse_calls_total"},
		"hard_parses_per_sec":      {"oracledb_hard_parses_total"},
		"logical_reads_per_sec":    {"oracledb_logical_reads_total"},
		"consistent_gets_per_sec":  {"oracledb_consistent_gets_total"},
		"db_block_gets_per_sec":    {"oracledb_db_block_gets_total"},
		"physical_reads_per_sec":   {"oracledb_physical_reads_total"},
		"cpu_time_seconds_per_sec": {"oracledb_cpu_time_seconds_total"},
		"tablespace_used_pct":      {"oracledb_tablespace_size_usage_bytes", "oracledb_tablespace_size_limit_bytes"},
	}
	metricName := regexp.MustCompile(`\boracle(?:db)?_[a-z_]+`)

	keyMetrics := dbExporterConfigs["oracle"].KeyMetrics
	if len(keyMetrics) != len(want) {
		t.Errorf("got %d oracle key metrics, want %d", len(keyMetrics), len(want))
	}
	for _, km := range keyMetrics {
		t.Run(km.Name, func(t *testing.T) {
			wantNames, ok := want[km.Name]
			if !ok {
				t.Fatalf("unexpected key %q (query %s)", km.Name, km.Query)
			}
			got := slices.DeleteFunc(metricName.FindAllString(km.Query, -1), func(name string) bool {
				return name == "oracledb_instance_name"
			})
			sort.Strings(got)
			got = slices.Compact(got)
			wantSorted := slices.Sorted(slices.Values(wantNames))
			if !slices.Equal(got, wantSorted) {
				t.Errorf("query %s references %v, want %v", km.Query, got, wantSorted)
			}
			if !strings.Contains(km.Query, "by (oracledb_instance_name)") {
				t.Errorf("query %s is not grouped by oracledb_instance_name", km.Query)
			}
		})
	}
}

func TestGetDatabaseServerMetricsHandler_OracleValuesPerInstance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "prom_label_values") {
			json.NewEncoder(w).Encode([]string{"oracledb_up"})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"metric": map[string]string{"oracledb_instance_name": "ORCL1"}, "value": []any{1700000000, "3"}},
			{"metric": map[string]string{"oracledb_instance_name": "ORCL2"}, "value": []any{1700000000, "5"}},
			{"metric": map[string]string{"oracledb_instance_name": "ORCL3"}, "value": []any{1700000000, "NaN"}},
		})
	}))
	defer server.Close()

	handler := NewGetDatabaseServerMetricsHandler(server.Client(), testDBConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:        "oracle",
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var response struct {
		Databases []struct {
			Metrics map[string]map[string]float64 `json:"metrics"`
		} `json:"databases"`
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", text, err)
	}
	got := response.Databases[0].Metrics["active_sessions"]
	want := map[string]float64{"ORCL1": 3, "ORCL2": 5}
	if !maps.Equal(got, want) {
		t.Errorf("active_sessions = %v, want %v (non-finite samples dropped)", got, want)
	}
}

func TestKeyMetricValue_NonFiniteSamples(t *testing.T) {
	series := func(instanceValues ...string) apiPromInstantResp {
		var resp apiPromInstantResp
		for i := 0; i < len(instanceValues); i += 2 {
			resp = append(resp, apiPromInstantResp{{
				Metric: map[string]string{"oracledb_instance_name": instanceValues[i]},
				Value:  []any{1700000000.0, instanceValues[i+1]},
			}}...)
		}
		return resp
	}
	scalar := dbKeyMetric{Name: "scalar"}
	grouped := dbKeyMetric{Name: "grouped", ByLabel: "oracledb_instance_name"}

	tests := []struct {
		name   string
		km     dbKeyMetric
		series apiPromInstantResp
		want   any
	}{
		{"scalar finite", scalar, series("", "2"), 2.0},
		{"scalar NaN", scalar, series("", "NaN"), nil},
		{"scalar no series", scalar, nil, nil},
		{"grouped mixed omits non-finite instance", grouped, series("ORCL1", "3", "ORCL3", "NaN", "ORCL4", "+Inf"), map[string]float64{"ORCL1": 3}},
		{"grouped all non-finite", grouped, series("ORCL1", "NaN", "ORCL2", "-Inf"), nil},
		{"grouped no series", grouped, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyMetricValue(tt.km, tt.series); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestGetDatabaseServerMetricsHandler_UnknownDBSystem(t *testing.T) {
	handler := NewGetDatabaseServerMetricsHandler(nil, testDBConfig("http://unused"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:        "cassandra",
		LookbackMinutes: 60,
	})
	if err == nil {
		t.Fatal("expected error for unsupported db_system, got nil")
	}
	if !strings.Contains(err.Error(), "unknown db_system") {
		t.Errorf("expected 'unknown db_system' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "cassandra") {
		t.Errorf("expected db_system name in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), supportedDBSystems) {
		t.Errorf("expected full supported list %q in error, got: %v", supportedDBSystems, err)
	}
}

func TestGetDatabaseServerMetricsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetDatabaseServerMetricsHandler(http.DefaultClient, *cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		LookbackMinutes: 60,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("get_database_server_metrics response (%d bytes): %.500s", len(text), text)
}
