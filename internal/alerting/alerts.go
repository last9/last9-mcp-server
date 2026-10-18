package alerting

import (
	"encoding/json"
)

// AlertRuleExpressionArg holds indicator binding for an alert rule.
// PromQL, Unit, and LookupError are resolved at query time and not part of the API response.
type AlertRuleExpressionArg struct {
	ID          string            `json:"id"`
	Variables   map[string]string `json:"variables"`
	PromQL      string            `json:"-"`
	Unit        string            `json:"-"`
	LookupError string            `json:"-"`
}

// AlertRule represents an alert configuration from Last9 API
type AlertRule struct {
	ID                           string                            `json:"id"`
	OrganizationID               string                            `json:"organization_id"`
	EntityID                     string                            `json:"entity_id"`
	PrimaryIndicator             string                            `json:"primary_indicator"`
	Expression                   string                            `json:"expression,omitempty"`
	Condition                    string                            `json:"condition,omitempty"`
	AlertCondition               string                            `json:"alert_condition,omitempty"`
	EvalWindow                   int64                             `json:"eval_window,omitempty"`
	ExpressionArgs               map[string]AlertRuleExpressionArg `json:"expression_args,omitempty"`
	CreatedAt                    int64                             `json:"created_at"`
	UpdatedAt                    int64                             `json:"updated_at"`
	DeletedAt                    *int64                            `json:"deleted_at"`
	ErrorSince                   *int64                            `json:"error_since"`
	State                        string                            `json:"state"`
	ExternalRef                  string                            `json:"external_ref"`
	Severity                     string                            `json:"severity"`
	Algorithm                    string                            `json:"algorithm"`
	RuleName                     string                            `json:"rule_name"`
	MuteUntil                    int64                             `json:"mute_until"`
	Properties                   map[string]interface{}            `json:"properties"`
	GroupTimeseriesNotifications bool                              `json:"group_timeseries_notifications"`
	RecurringMuteSchedule        json.RawMessage                   `json:"recurring_mute_schedule,omitempty"`
	// RecentEvaluations is sampled from the alert monitor by get_alert_config, not part of the API response.
	RecentEvaluations []evaluationSample `json:"-"`
}

// AlertConfigResponse represents the response from alert rules API (direct array)
type AlertConfigResponse []AlertRule
