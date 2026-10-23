package remapping

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const integrationTestTimeout = 30 * time.Second
const integrationTestRuleName = "mcp-local-integration-test"

func TestGetRemappingRules_Integration_AllTypes(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetRemappingRulesHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	for _, ruleType := range []string{ruleTypeLogsExtract, ruleTypeLogsMap, ruleTypeTracesMap} {
		t.Run(ruleType, func(t *testing.T) {
			result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetRemappingRulesArgs{
				RuleType: ruleType,
			})
			if utils.CheckAPIError(t, err) {
				return
			}
			if result == nil {
				t.Fatal("expected result, got nil")
			}

			text := utils.GetTextContent(t, result)
			if !json.Valid([]byte(text)) {
				t.Fatalf("expected valid JSON response, got: %s", text)
			}

			if ref, ok := result.Meta["reference_url"].(string); !ok || ref == "" {
				t.Fatal("expected reference_url in result meta")
			} else if !strings.Contains(ref, "/remapping") {
				t.Fatalf("expected remapping deep link, got %q", ref)
			}

			t.Logf("%s: response length %d bytes", ruleType, len(text))
		})
	}
}

func TestAddRemappingRule_Integration_IdempotentJSONExtract(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	getHandler := NewGetRemappingRulesHandler(http.DefaultClient, *cfg)
	addHandler := NewAddRemappingRuleHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	existing, _, err := getHandler(ctx, &mcp.CallToolRequest{}, GetRemappingRulesArgs{
		RuleType: ruleTypeLogsExtract,
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	if strings.Contains(utils.GetTextContent(t, existing), integrationTestRuleName) {
		t.Skipf("rule %q already exists; skipping create", integrationTestRuleName)
	}

	result, _, err := addHandler(ctx, &mcp.CallToolRequest{}, AddRemappingRuleArgs{
		RuleType:        ruleTypeLogsExtract,
		Name:            integrationTestRuleName,
		ExtractType:     "json",
		RemapKeys:       []string{"level"},
		TargetAttribute: "log_attributes",
		Action:          "upsert",
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &created); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	if created.Name != integrationTestRuleName {
		t.Fatalf("expected name %q, got %q", integrationTestRuleName, created.Name)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty rule id in create response")
	}

	after, _, err := getHandler(ctx, &mcp.CallToolRequest{}, GetRemappingRulesArgs{
		RuleType: ruleTypeLogsExtract,
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	if !strings.Contains(utils.GetTextContent(t, after), integrationTestRuleName) {
		t.Fatalf("created rule %q not found in subsequent list", integrationTestRuleName)
	}

	t.Logf("created remapping rule id=%s name=%s", created.ID, created.Name)
}
