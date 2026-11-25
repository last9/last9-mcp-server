package logs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// LogAttributeScope narrows attribute discovery to a service and a window;
// an empty Service lists the workspace's indexed attributes.
type LogAttributeScope struct {
	Service         string
	LookbackMinutes int
	StartTimeISO    string
	EndTimeISO      string
}

// DiscoverLogAttributes returns the attributes get_log_attributes_for_pipeline
// reports for a ServiceName filter, or the indexed ones get_log_attributes
// lists when no service is given.
func DiscoverLogAttributes(ctx context.Context, client *http.Client, cfg models.Config, scope LogAttributeScope) ([]LogAttribute, error) {
	startTime, endTime, err := logAttributeWindow(scope.LookbackMinutes, scope.StartTimeISO, scope.EndTimeISO)
	if err != nil {
		return nil, err
	}
	queryParams := url.Values{}
	queryParams.Set("region", cfg.Region)
	queryParams.Set("start", fmt.Sprintf("%d", startTime))
	queryParams.Set("end", fmt.Sprintf("%d", endTime))

	if scope.Service == "" {
		names, err := fetchLogLabels(ctx, client, cfg, queryParams)
		if err != nil {
			return nil, err
		}
		out := make([]LogAttribute, 0, len(names))
		for _, name := range names {
			filterField := logFieldFilterField(name)
			out = append(out, LogAttribute{Name: name, FilterField: filterField, Hint: utils.EQExample(filterField, "<value>")})
		}
		return out, nil
	}

	pipeline := []map[string]interface{}{{
		"type":  "filter",
		"query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"ServiceName", scope.Service}}}},
	}}
	return discoverLogAttributes(ctx, client, cfg, pipeline, startTime, endTime, "", queryParams)
}

// logAttributeWindow resolves an attribute-discovery window in Unix seconds,
// capped at MaxLogAttributeLookbackMinutes: a longer range only adds server cost.
func logAttributeWindow(lookbackMinutes int, startTimeISO, endTimeISO string) (int64, int64, error) {
	params := make(map[string]interface{})
	if lookbackMinutes > 0 {
		params["lookback_minutes"] = lookbackMinutes
	}
	if startTimeISO != "" {
		params["start_time_iso"] = startTimeISO
	}
	if endTimeISO != "" {
		params["end_time_iso"] = endTimeISO
	}

	const defaultLogAttributesLookback = 15
	startTimeParsed, endTimeParsed, err := utils.GetTimeRange(params, defaultLogAttributesLookback)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse time range: %w", err)
	}
	endTime := endTimeParsed.Unix()
	startTime := max(startTimeParsed.Unix(), endTime-int64(utils.MaxLogAttributeLookbackMinutes*60))
	return startTime, endTime, nil
}
