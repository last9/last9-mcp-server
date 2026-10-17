package alerting

import (
	"context"
	"strings"
	"testing"

	"last9-mcp/internal/models"
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
	for _, want := range []string{"static", "is_disabled=true", "runbook", "annotations", "patch_alert"} {
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
