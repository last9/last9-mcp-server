package alerting

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RecommendAlertConfigArgs struct {
	Signal      string `json:"signal" jsonschema:"(Required) Metric or user-impact signal to alert on, including its unit and relevant dimensions"`
	Objective   string `json:"objective" jsonschema:"(Required) Operational symptom or user impact this alert should detect"`
	RuleType    string `json:"rule_type,omitempty" jsonschema:"Alert type: static or anomaly (default: static); anomaly means an adaptive_std_cmp(metric, sensitivity, window) rule"`
	Environment string `json:"environment,omitempty" jsonschema:"Environment and traffic profile, for example production with weekday seasonality"`
	Severity    string `json:"severity,omitempty" jsonschema:"Desired severity: breach or threat (default: breach)"`
}

func NewRecommendAlertConfigHandler(_ *http.Client, _ models.Config) func(context.Context, *mcp.CallToolRequest, RecommendAlertConfigArgs) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, args RecommendAlertConfigArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Signal) == "" {
			return utils.ToolErrorResult("signal is required"), nil, nil
		}
		if strings.TrimSpace(args.Objective) == "" {
			return utils.ToolErrorResult("objective is required"), nil, nil
		}
		ruleType := strings.ToLower(strings.TrimSpace(args.RuleType))
		if ruleType == "" {
			ruleType = alertConfigRuleTypeStatic
		}
		if ruleType != alertConfigRuleTypeStatic && ruleType != alertConfigRuleTypeAnomaly {
			return utils.ToolErrorResult("rule_type must be one of \"static\" or \"anomaly\""), nil, nil
		}
		severity := strings.ToLower(strings.TrimSpace(args.Severity))
		if severity == "" {
			severity = severityBreach
		}
		if severity != severityBreach && severity != severityThreat {
			return utils.ToolErrorResult("severity must be one of \"breach\" or \"threat\""), nil, nil
		}

		strategy := map[string]string{
			alertConfigRuleTypeStatic:  "Use a threshold derived from an SLO, capacity limit, or historical percentile; never invent a threshold. Start with 3 bad minutes in a 5-minute evaluation window and tune from observed incidents.",
			alertConfigRuleTypeAnomaly: "Use adaptive_std_cmp(metric, sensitivity, window) when the signal has a stable learned baseline. Confirm seasonality, minimum traffic, model warm-up, and a static safety bound before enabling paging.",
		}[ruleType]

		text := fmt.Sprintf(`Recommended %s alert configuration

Signal: %s
Objective: %s
Environment: %s
Severity: %s

Strategy: %s

Lifecycle:
1. Discover and verify the alert group, labels, and alert-group notification routing. Take KPI ids from get_entity_kpis; if none fits, create one once with create_kpi. Never invent a KPI id.
2. Check each KPI query and its unit against real data with prometheus_range_query; thresholds compare against the raw value.
3. Run validate_alert_expression until it returns {"valid":true}.
4. Create the rule once (create_alert always creates it disabled, is_disabled=true). The backend derives the algorithm from expression; clients must not send algorithm.
5. Read the rule back with get_entity_alert_rules and confirm it matches the design.
6. Observe or replay representative traffic, checking no-data behavior, cardinality, grouped notifications, and recovery.
7. Promote it with patch_alert by setting is_disabled=false, only after the user confirms; PATCH preserves the rule ID.
8. Tune evaluation configuration with update_alert, use patch_alert for mute/pause/metadata, and delete only after explicit confirmation.

Grammar, units, and static/adaptive templates: last9://reference/alerting.

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
