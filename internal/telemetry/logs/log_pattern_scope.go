package logs

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// LogPatternScopeArgs selects the lines a pattern tool clusters. Both pattern
// tools embed it, so their scope parameters cannot drift apart.
type LogPatternScopeArgs struct {
	ServiceName     string   `json:"service_name" jsonschema:"(Required) Service whose logs to cluster (e.g. checkout)"`
	Env             string   `json:"env,omitempty" jsonschema:"Environment to filter by (e.g. production). Omit when unknown."`
	StartTimeISO    string   `json:"start_time_iso,omitempty" jsonschema:"Window start, RFC3339 (e.g. 2026-10-16T10:00:00Z). If omitted, lookback_minutes is used."`
	EndTimeISO      string   `json:"end_time_iso,omitempty" jsonschema:"Window end, RFC3339. Defaults to now."`
	LookbackMinutes int      `json:"lookback_minutes,omitempty" jsonschema:"Window length in minutes when start_time_iso is omitted (default: 60, minimum: 1)"`
	SeverityFilters []string `json:"severity_filters,omitempty" jsonschema:"Severities to keep, OR logic, case-insensitive (e.g. [error warn]). Omit to cluster every severity."`
	BodyFilters     []string `json:"body_filters,omitempty" jsonschema:"Message substrings to keep, OR logic, case-insensitive (e.g. [timeout])"`
	Index           string   `json:"index,omitempty" jsonschema:"Optional log index, physical_index:<name> or rehydration_index:<block_name>, as discovered by get_service_profile. Omit for the default index."`
	SampleSize      int      `json:"sample_size,omitempty" jsonschema:"Maximum log lines sampled per window (default: 1000, maximum: 5000). Counts are within the sample."`
}

const (
	defaultPatternSampleSize = 1000
	maxPatternSampleSize     = utils.LogSearchMaxSampleSize
	defaultPatternLookback   = 60
	defaultPatternTopN       = 20
	maxPatternTopN           = 100
)

// LogPatternWindow describes the sample one window's patterns were mined from.
type LogPatternWindow struct {
	StartTime          string `json:"start_time"`
	EndTime            string `json:"end_time"`
	SampledLines       int    `json:"sampled_lines"`
	SampleLimitReached bool   `json:"sample_limit_reached"`
	TotalMatchingLines *int   `json:"total_matching_lines,omitempty"`
	LogsTruncated      *bool  `json:"logs_truncated,omitempty"`
}

type logPatternScope struct {
	service    string
	env        string
	query      []map[string]interface{}
	index      string
	sampleSize int
}

func newLogPatternScope(args LogPatternScopeArgs) (logPatternScope, error) {
	service := strings.TrimSpace(args.ServiceName)
	if service == "" {
		return logPatternScope{}, fmt.Errorf("service_name is required")
	}
	sampleSize, err := boundedPatternLimit("sample_size", args.SampleSize, defaultPatternSampleSize, maxPatternSampleSize)
	if err != nil {
		return logPatternScope{}, err
	}
	index, err := utils.NormalizeLogIndex(args.Index)
	if err != nil {
		return logPatternScope{}, fmt.Errorf("invalid index: %w", err)
	}
	env := strings.TrimSpace(args.Env)
	query := buildServiceLogsQuery(service, args.SeverityFilters, args.BodyFilters)
	query = addServiceLogsEnvFilter(query, env)
	return logPatternScope{service: service, env: env, query: query, index: index, sampleSize: sampleSize}, nil
}

func (args LogPatternScopeArgs) timeRange() (time.Time, time.Time, error) {
	params := map[string]interface{}{}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}
	if args.LookbackMinutes != 0 {
		params["lookback_minutes"] = args.LookbackMinutes
	}
	start, end, err := utils.GetTimeRange(params, defaultPatternLookback)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid time range: %w", err)
	}
	// An empty window has nothing to cluster and would scale rates by zero.
	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid time range: start_time_iso must be before end_time_iso")
	}
	return start, end, nil
}

func (s logPatternScope) sample(ctx context.Context, client *http.Client, cfg models.Config, start, end time.Time) ([]LogEntry, LogPatternWindow, error) {
	logs, err := fetchServiceLogs(ctx, client, cfg, s.service, start, end, s.sampleSize, cloneLogJSONQuery(s.query), s.index)
	if err != nil {
		return nil, LogPatternWindow{}, fmt.Errorf("failed to sample logs: %w", err)
	}
	// A window missing a chunk would read as fewer or absent patterns, i.e.
	// false novelty, so it fails like the chunked path does.
	if failed, _ := logs.SearchStats["chunks_failed"].(float64); failed > 0 {
		return nil, LogPatternWindow{}, fmt.Errorf("log search for %s to %s is incomplete: %d of its search chunks failed; retry or narrow the window",
			start.Format(time.RFC3339), end.Format(time.RFC3339), int(failed))
	}
	return logs.Logs, LogPatternWindow{
		StartTime:          start.Format(time.RFC3339),
		EndTime:            end.Format(time.RFC3339),
		SampledLines:       len(logs.Logs),
		SampleLimitReached: sampleIsPartial(logs, s.sampleSize),
		TotalMatchingLines: logs.TotalMatchingLines,
		LogsTruncated:      logs.LogsTruncated,
	}, nil
}

// sampleIsPartial reports whether the sample may miss matching lines. An exact
// total settles it; a truncated search's total is only a floor, and without a
// total a full sample is assumed capped.
func sampleIsPartial(logs *ServiceLogsResponse, sampleSize int) bool {
	switch {
	case logs.LogsTruncated != nil && *logs.LogsTruncated:
		return true
	case logs.TotalMatchingLines != nil:
		return *logs.TotalMatchingLines > len(logs.Logs)
	default:
		return len(logs.Logs) >= sampleSize
	}
}

func boundedPatternLimit(name string, value, fallback, max int) (int, error) {
	switch {
	case value == 0:
		return fallback, nil
	case value < 0 || value > max:
		return 0, fmt.Errorf("%s must be between 1 and %d, got %d", name, max, value)
	default:
		return value, nil
	}
}

// rankLogPatterns orders items by compare, breaking ties by template so every
// pattern tool ranks the same way, and splits off the items past topN.
func rankLogPatterns[T any](items []T, topN int, template func(T) string, compare func(a, b T) int) (top, rest []T) {
	slices.SortStableFunc(items, func(a, b T) int {
		return cmp.Or(compare(a, b), strings.Compare(template(a), template(b)))
	})
	if len(items) <= topN {
		return items, nil
	}
	return items[:topN], items[topN:]
}
