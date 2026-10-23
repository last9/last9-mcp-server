package alerts

import (
	"context"
	"encoding/json"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathHealthSummary = "/alerts/health_summary"

const opHealthSummary = "alert health summary"

const maxHealthSummaryBytes = 1 << 20

type GetHealthSummaryInput struct {
	// Unix seconds.
	From       int64    `json:"from"`
	To         int64    `json:"to"`
	Timezone   string   `json:"timezone"`
	Services   []string `json:"services,omitempty"`
	Severities []string `json:"severities,omitempty"`
}

// HealthMetric is how the API reports every count: Count is meaningful only with Status.
type HealthMetric struct {
	Status string `json:"status"`
	Count  *int64 `json:"count,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type HealthDelivered struct {
	Total *HealthMetric `json:"total"`
	Slack *HealthMetric `json:"slack"`
	Email *HealthMetric `json:"email"`
}

type GetHealthSummaryResponse struct {
	SchemaVersion        *int             `json:"schema_version"`
	AlertsRaised         *HealthMetric    `json:"alerts_raised"`
	Acknowledged         *HealthMetric    `json:"acknowledged"`
	Unacknowledged       *HealthMetric    `json:"unacknowledged"`
	CurrentBacklog       *HealthMetric    `json:"current_backlog"`
	Delivered            *HealthDelivered `json:"delivered"`
	NotificationOutcomes json.RawMessage  `json:"notification_outcomes"`
	TopRules             json.RawMessage  `json:"top_rules"`
	NeedsAttention       json.RawMessage  `json:"needs_attention"`
	Coverage             json.RawMessage  `json:"coverage"`
}

// GetHealthSummary calls POST /alerts/health_summary.
func GetHealthSummary(ctx context.Context, c *last9api.Client, in GetHealthSummaryInput) (GetHealthSummaryResponse, error) {
	var resp GetHealthSummaryResponse
	err := c.Do(ctx, opHealthSummary,
		last9api.Request{Method: http.MethodPost, Path: pathHealthSummary, Body: in}, &resp,
		last9api.WithMaxResponseBytes(maxHealthSummaryBytes))
	return resp, err
}
