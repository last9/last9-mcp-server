package alerting

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Live parity checks against a real org (.env). Run:
// go test ./internal/alerting -run TestLiveNotificationChannelParity -count=1 -v
func TestLiveNotificationChannelParity(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetAlertConfigHandler(http.DefaultClient, *cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	run := func(label string, args GetAlertConfigArgs) string {
		result, _, err := handler(ctx, &mcp.CallToolRequest{}, args)
		if utils.CheckAPIError(t, err) {
			t.Fatalf("%s: %v", label, err)
		}
		return utils.GetTextContent(t, result)
	}

	all := run("all rules", GetAlertConfigArgs{})
	countRules := func(text string) int {
		if strings.Contains(text, "Found 0 alert rules") {
			return 0
		}
		var n int
		if _, err := fmt.Sscanf(strings.Split(text, "\n")[0], "Found %d alert rules:", &n); err == nil {
			return n
		}
		return strings.Count(text, "\n  ID: ")
	}

	t.Logf("all rules: %d", countRules(all))
	if !strings.Contains(all, "Notification Channels:") {
		t.Fatalf("missing Notification Channels enrichment on default listing")
	}

	// Sample one configured and one unconfigured rule from the listing.
	var configuredSample, unconfiguredSample string
	for _, block := range strings.Split(all, "Alert Rule ") {
		if !strings.Contains(block, "ID:") {
			continue
		}
		if strings.Contains(block, "Notification Channels: Not configured") {
			if unconfiguredSample == "" {
				unconfiguredSample = truncateLiveSample(block, 900)
			}
		} else if configuredSample == "" {
			configuredSample = truncateLiveSample(block, 900)
		}
	}
	if configuredSample != "" {
		t.Logf("configured sample:\n%s", configuredSample)
	} else {
		t.Log("no configured sample found in org (all Not configured?)")
	}
	if unconfiguredSample != "" {
		t.Logf("unconfigured sample:\n%s", unconfiguredSample)
	}

	unconfigured := run("only_without", GetAlertConfigArgs{OnlyWithoutNotificationChannel: true})
	t.Logf("only_without: %d rules", countUnconfiguredRules(unconfigured))
	if !strings.Contains(unconfigured, "Global notification channels:") {
		t.Fatalf("expected global advisory on unconfigured filter")
	}

	slackBreach := run("slack+breach", GetAlertConfigArgs{
		NotificationChannelTypes:      []string{"slack"},
		NotificationChannelSeverities: []string{"breach"},
	})
	t.Logf("slack+breach bindings: %d rules", countRules(slackBreach))

	slack := run("slack type", GetAlertConfigArgs{NotificationChannelTypes: []string{"slack"}})
	t.Logf("slack bindings: %d rules", countRules(slack))

	named := run("track-alerts name", GetAlertConfigArgs{NotificationChannelNames: []string{"track-alerts"}})
	t.Logf("track-alerts name: %d rules", countRules(named))
}

func countUnconfiguredRules(text string) int {
	line := strings.Split(text, "\n")[0]
	if strings.Contains(line, "with no per-entity") {
		var n int
		if _, err := fmt.Sscanf(line, "Found %d alert rule(s) with no per-entity notification channel configured:", &n); err == nil {
			return n
		}
	}
	return strings.Count(text, "\n  ID: ")
}

func truncateLiveSample(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
