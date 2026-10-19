package alerts

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

const pathAlertCoverage = "/alerts/coverage"

const opAlertCoverage = "alert coverage"

// The response holds one row per gap, so it grows with the org; past this the caller
// narrows by service, environment or entity instead of receiving an unbounded payload.
const maxCoverageResponseBytes = 4 << 20

const (
	CoverageGapUncoveredSignal       = "uncovered_signal"
	CoverageGapAllRulesDisabled      = "all_rules_disabled"
	CoverageGapNoNotificationChannel = "no_notification_channel"
	CoverageGapMutedTooLong          = "muted_too_long"
)

type GetAlertCoverageInput struct {
	Service     string
	Environment string
	EntityID    string
	MutedDays   int
}

// MutedSince and MuteUntil are unix seconds, sent only on muted_too_long gaps (absent
// decodes to 0); MuteUntil is -1 for an indefinite mute.
type CoverageGap struct {
	Kind          string `json:"kind"`
	EntityID      string `json:"entity_id"`
	EntityName    string `json:"entity_name"`
	Service       string `json:"service"`
	Signal        string `json:"signal"`
	RuleID        string `json:"rule_id"`
	RuleName      string `json:"rule_name"`
	Severity      string `json:"severity"`
	MutedSince    int64  `json:"muted_since"`
	MuteUntil     int64  `json:"mute_until"`
	Detail        string `json:"detail"`
	SuggestedTool string `json:"suggested_tool"`
}

// GeneratedAt is unix seconds.
type GetAlertCoverageResponse struct {
	GeneratedAt int64          `json:"generated_at"`
	MutedDays   int            `json:"muted_days"`
	Gaps        []CoverageGap  `json:"gaps"`
	Counts      map[string]int `json:"counts"`
}

// GetAlertCoverage calls GET /alerts/coverage?service=&environment=&entity_id=&muted_days=.
func GetAlertCoverage(
	ctx context.Context, c *last9api.Client, in GetAlertCoverageInput,
) (*GetAlertCoverageResponse, error) {
	var out *GetAlertCoverageResponse
	err := c.Do(ctx, opAlertCoverage,
		last9api.Request{Method: http.MethodGet, Path: pathAlertCoverage},
		&out,
		last9api.WithQuery(coverageQuery(in)),
		last9api.WithMaxResponseBytes(maxCoverageResponseBytes),
	)
	if err != nil {
		return nil, err
	}
	// Do accepts an empty 2xx body, and the API always sends counts; a body with neither
	// gaps nor counts, read as a zero-gap report, would claim full coverage.
	if out == nil || (out.Counts == nil && len(out.Gaps) == 0) {
		return nil, errors.New(opAlertCoverage + ": response held no coverage report, so coverage was not checked")
	}
	return out, nil
}

func coverageQuery(in GetAlertCoverageInput) url.Values {
	q := url.Values{}
	for key, value := range map[string]string{
		"service":     in.Service,
		"environment": in.Environment,
		"entity_id":   in.EntityID,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if in.MutedDays > 0 {
		q.Set("muted_days", strconv.Itoa(in.MutedDays))
	}
	return q
}
