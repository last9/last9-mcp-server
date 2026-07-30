package traces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A verbatim copy of the endpoint's published example response. Shared artifact: if
// the API renames a field, refreshing this file fails the assertions below instead of
// silently turning the tool description into a wrong manual.
const deviationEndpointFixturePath = "../../../contracts/fixtures/evidence-attribute-deviations-endpoint.json"

func deviationEndpointFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(deviationEndpointFixturePath))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func callDeviationsHandler(t *testing.T, status int, body []byte) (string, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), deviationTestConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "checkout", Environment: "production",
	})
	if err != nil {
		return "", err
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", result.Content[0])
	}
	return text.Text, nil
}

// The upstream body is what reaches the model, so hold it to the same schema the
// waterfall producer is held to.
func TestDeviationEndpointFixtureSatisfiesEvidenceContract(t *testing.T) {
	schema := resolvedEvidenceSchema(t)
	forwarded, err := callDeviationsHandler(t, http.StatusOK, deviationEndpointFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(forwarded), &payload); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(payload); err != nil {
		t.Fatalf("endpoint response does not satisfy %s: %v", evidenceSchemaPath, err)
	}
	if payload["contract_version"] != investigationEvidenceVersion {
		t.Fatalf("contract_version=%v", payload["contract_version"])
	}
	if payload["analysis_version"] != attributeDeviationsVersion {
		t.Fatalf("analysis_version=%v", payload["analysis_version"])
	}
	interpretation := payload["interpretation"].(map[string]any)
	if interpretation["claim_type"] == "cause" {
		t.Fatal("causal claim type is forbidden")
	}
}

// Each entry is a field the tool description promises the model it will receive.
func TestDeviationDescriptionClaimsMatchEndpointFixture(t *testing.T) {
	fixture := string(deviationEndpointFixture(t))
	for _, field := range []string{
		`"target_share"`, `"control_share"`, `"percentage_point_delta"`,
		// Nullable upstream, so the description must not promise a finite one.
		`"ratio"`,
		`"target_missing"`, `"control_missing"`, `"rank"`,
		`"partial"`, `"truncated"`, `"warnings"`, `"evidence_quality"`,
	} {
		if !strings.Contains(fixture, field) {
			t.Fatalf("description promises %s but the endpoint fixture has no such field", field)
		}
	}
	// The description tells the model the endpoint never claims cause.
	if strings.Contains(fixture, `"claim_type": "cause"`) {
		t.Fatal("endpoint fixture claims cause")
	}
}

// These strings live in the endpoint, so pin them: a rename must fail here rather
// than leave the description pointing the model at a string that is gone.
func TestDeviationDescriptionWarningStringsAreDocumented(t *testing.T) {
	description, err := os.ReadFile(filepath.Clean("../../prompts/descriptions/get_trace_attribute_deviations.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range []string{
		"candidate_limit_reached",
		"minimum_cohort_size_not_met",
		"attribute_value_limit_reached",
	} {
		if !strings.Contains(string(description), warning) {
			t.Fatalf("description must document the %q warning", warning)
		}
	}
	// excluded_reason values, not warnings — the description must not conflate them.
	for _, reason := range []string{
		"distinct_value_limit_exceeded",
		"sensitive_or_high_cardinality_policy",
	} {
		if !strings.Contains(string(description), reason) {
			t.Fatalf("description must document the %q exclusion reason", reason)
		}
	}
}

func TestDeviationNonConformingResponseIsRejected(t *testing.T) {
	cases := map[string]string{
		"wrong contract version": `{"contract_version":"investigation-evidence/v2","analysis_version":"trace-attribute-deviations/v1"}`,
		"missing contract":       `{"data":{"deviations":[]}}`,
		"wrong analysis version": `{"contract_version":"investigation-evidence/v1","analysis_version":"trace-waterfall/v1"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := callDeviationsHandler(t, http.StatusOK, []byte(body)); err == nil {
				t.Fatal("expected a non-conforming response to be rejected")
			}
		})
	}
}
