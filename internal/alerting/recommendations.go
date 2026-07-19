package alerting

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RecommendAlertConfigArgs struct {
	Signal      string `json:"signal" jsonschema:"(Required) Metric or user-impact signal to alert on, including its unit and relevant dimensions"`
	Objective   string `json:"objective" jsonschema:"(Required) Operational symptom or user impact this alert should detect"`
	RuleType    string `json:"rule_type,omitempty" jsonschema:"Alert type: static or anomaly (default: static). Adaptive is not a distinct creatable backend type."`
	Environment string `json:"environment,omitempty" jsonschema:"Environment and traffic profile, for example production with weekday seasonality"`
	Severity    string `json:"severity,omitempty" jsonschema:"Desired severity: breach or threat (default: breach)"`
}

func NewRecommendAlertConfigHandler(_ *http.Client, _ models.Config) func(context.Context, *mcp.CallToolRequest, RecommendAlertConfigArgs) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, args RecommendAlertConfigArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Signal) == "" {
			return toolErrorResult("signal is required"), nil, nil
		}
		if strings.TrimSpace(args.Objective) == "" {
			return toolErrorResult("objective is required"), nil, nil
		}
		ruleType := strings.ToLower(strings.TrimSpace(args.RuleType))
		if ruleType == "" {
			ruleType = alertConfigRuleTypeStatic
		}
		if ruleType != alertConfigRuleTypeStatic && ruleType != alertConfigRuleTypeAnomaly {
			return toolErrorResult("rule_type must be one of \"static\" or \"anomaly\"; adaptive is currently evaluated as static by the backend"), nil, nil
		}
		severity := strings.ToLower(strings.TrimSpace(args.Severity))
		if severity == "" {
			severity = "breach"
		}
		if severity != "breach" && severity != "threat" {
			return toolErrorResult("severity must be one of \"breach\" or \"threat\""), nil, nil
		}

		strategy := map[string]string{
			alertConfigRuleTypeStatic:  "Use a threshold derived from an SLO, capacity limit, or historical percentile; never invent a threshold. Start with 3 bad minutes in a 5-minute evaluation window and tune from observed incidents.",
			alertConfigRuleTypeAnomaly: "Use only a backend-supported anomaly macro when the signal has a stable learned baseline. Confirm seasonality, minimum traffic, model warm-up, and a static safety bound before enabling paging.",
		}[ruleType]

		text := fmt.Sprintf(`Recommended %s alert configuration

Signal: %s
Objective: %s
Environment: %s
Severity: %s

Strategy: %s

Lifecycle:
1. Discover and verify the alert group, KPI/query, unit, labels, and alert-group notification routing.
2. Create the rule disabled (is_disabled=true). The backend derives the algorithm from expression; clients must not send algorithm.
3. Observe or replay representative traffic, checking no-data behavior, cardinality, grouped notifications, and recovery.
4. Promote it with patch_alert by setting is_disabled=false; PATCH preserves the rule ID.
5. Tune evaluation configuration with update_alert, use patch_alert for mute/pause/metadata, and delete only after explicit confirmation.

Required responder context under properties:
- description: symptom, impact, and intended responder action
- runbook: {"link":"https://..."}
- annotations: owner, service, environment, summary, dashboard, logs, traces, escalation policy; templates may use {{ $labels.<name> }} and {{ $value }}

Notification routing is configured on the alert group, not inside the alert rule.

Quality gates: actionable and user-impacting; resistant to low-volume/no-data noise; bounded cardinality; grouped where appropriate; recovery notifications enabled; linked dashboard/runbook; distinct warning/page severities; threshold or sensitivity justified by evidence.`,
			ruleType, args.Signal, args.Objective, defaultText(args.Environment, "not supplied — inspect before promotion"), severity, strategy)

		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	}
}

func defaultText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
