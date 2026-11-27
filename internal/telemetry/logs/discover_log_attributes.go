package logs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

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
	discovery, err := scanLogAttributes(ctx, client, cfg, pipeline, startTime, endTime, "", queryParams)
	return discovery.Attributes, err
}

// logAttributeWindow resolves an attribute-discovery window in Unix seconds.
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
	return startTimeParsed.Unix(), endTimeParsed.Unix(), nil
}

const maxLogAttributeScanSlices = 24

type logAttributeDiscovery struct {
	Attributes    []LogAttribute `json:"attributes"`
	ScannedStart  int64          `json:"-"`
	ScannedEnd    int64          `json:"-"`
	ScannedWindow struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"scanned_window"`
	Note string `json:"note,omitempty"`
}

func newLogAttributeDiscovery(attributes []LogAttribute, start, end int64, note string) logAttributeDiscovery {
	out := logAttributeDiscovery{Attributes: attributes, ScannedStart: start, ScannedEnd: end, Note: note}
	out.ScannedWindow.Start = time.Unix(start, 0).UTC().Format(time.RFC3339)
	out.ScannedWindow.End = time.Unix(end, 0).UTC().Format(time.RFC3339)
	return out
}

func scanLogAttributes(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, requestedStart, requestedEnd int64, index string, baseQuery url.Values) (logAttributeDiscovery, error) {
	var lastStart int64
	for i, sliceEnd := 0, requestedEnd; i < maxLogAttributeScanSlices && sliceEnd > requestedStart; i++ {
		sliceStart := max(requestedStart, sliceEnd-int64(utils.MaxLogAttributeLookbackMinutes*60))
		query := cloneURLValues(baseQuery)
		query.Set("start", fmt.Sprintf("%d", sliceStart))
		query.Set("end", fmt.Sprintf("%d", sliceEnd))
		attributes, matched, err := discoverLogAttributes(ctx, client, cfg, pipeline, sliceStart, sliceEnd, index, query)
		if err != nil {
			return logAttributeDiscovery{}, err
		}
		lastStart = sliceStart
		if matched {
			return newLogAttributeDiscovery(attributes, sliceStart, sliceEnd, ""), nil
		}
		sliceEnd = sliceStart
	}
	note := fmt.Sprintf("No rows matched the pipeline in %s to %s. Discovery scans at most %d minutes per slice and %d slices.",
		time.Unix(lastStart, 0).UTC().Format(time.RFC3339), time.Unix(requestedEnd, 0).UTC().Format(time.RFC3339),
		utils.MaxLogAttributeLookbackMinutes, maxLogAttributeScanSlices)
	return newLogAttributeDiscovery([]LogAttribute{}, lastStart, requestedEnd, note), nil
}

func cloneURLValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}
