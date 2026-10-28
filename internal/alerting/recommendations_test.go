package alerting

import (
	"context"
	"strings"
	"testing"

	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"
)

func TestRecommendAlertConfig(t *testing.T) {
	result, _, err := NewRecommendAlertConfigHandler(nil, models.Config{})(context.Background(), nil, RecommendAlertConfigArgs{
		Signal:    "request error ratio (%)",
		Objective: "detect sustained checkout failures",
		RuleType:  "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(result)
	for _, want := range []string{"static", "is_disabled=true", "runbook", "annotations", "create_alert", "patch_alert", "update_alert"} {
		if !strings.Contains(text, want) {
			t.Errorf("recommendation missing %q", want)
		}
	}
	lifecycle := strings.SplitN(text, "Lifecycle:", 2)[1]
	last := -1
	for _, step := range []string{"get_entity_kpis", "create_kpi", "prometheus_range_query", `{"valid":true}`, "create_alert", "Read the rule back", "only after the user confirms"} {
		i := strings.Index(lifecycle, step)
		if i < 0 || i < last {
			t.Fatalf("lifecycle must name %q in order:\n%s", step, lifecycle)
		}
		last = i
	}
}

// The lifecycle names a write tool only when the server serves it, so a
// read-only server never sends the model to a tool it rejects as unknown.
func TestRecommendAlertConfigNamesOnlyServedWriteTools(t *testing.T) {
	tests := []struct {
		name   string
		filter toolsets.Filter
		named  []string
		hidden []string
	}{
		{name: "read-only", filter: toolsets.Filter{ReadOnly: true}, hidden: []string{"create_alert", "patch_alert", "update_alert"}},
		{name: "create_alert allowed", filter: toolsets.Filter{ReadOnly: true, AllowWrite: toolsets.Set{"create_alert": {}}}, named: []string{"create_alert"}, hidden: []string{"patch_alert", "update_alert"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := models.Config{ToolFilter: tt.filter}
			result, _, err := NewRecommendAlertConfigHandler(nil, cfg)(context.Background(), nil, RecommendAlertConfigArgs{
				Signal: "request error ratio (%)", Objective: "detect sustained checkout failures", RuleType: "static",
			})
			if err != nil {
				t.Fatal(err)
			}
			text := resultText(result)
			for _, want := range append([]string{"is_disabled=true", "is_disabled=false"}, tt.named...) {
				if !strings.Contains(text, want) {
					t.Errorf("recommendation missing %q:\n%s", want, text)
				}
			}
			for _, tool := range tt.hidden {
				if strings.Contains(text, tool) {
					t.Errorf("recommendation names unserved %s:\n%s", tool, text)
				}
			}
		})
	}
}
