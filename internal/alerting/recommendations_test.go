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
}
