package apm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serviceSummaryDefaultEnv     = ".*"
	serviceSummaryDefaultLimit   = 10
	serviceSummaryMaxLimit       = 100
	serviceSummarySpanKind       = `span_kind="SPAN_KIND_SERVER"`
	serviceSummaryGRPCMatcher    = `grpc_status_code!="",grpc_status_code!~"^(0|OK)$"`
	serviceSummaryCoverage       = "Ranking covers APM server-span metrics only. Log-only services are absent from rows, not listed as zero. Dual-instrumented services (multiple l9_source series for the same service and env) are summed together, so request_count and class counts can be inflated versus a single instrumentation source."
	serviceSummaryEmptyHint      = "No APM server-span series matched this interval and env_scope. rows is empty; the interval was not widened and no placeholder service names were added."
	serviceSummarySortRequest    = "request_count"
	serviceSummarySortRPM        = "throughput_rpm"
	serviceSummarySort4xx        = "http_4xx_count"
	serviceSummarySort5xx        = "http_5xx_count"
	serviceSummarySortGRPC       = "grpc_error_count"
	serviceSummaryFingerprintEnv = "<env>"
)

type ServiceSummaryRow struct {
	Rank           int     `json:"rank"`
	Service        string  `json:"service"`
	Env            string  `json:"env"`
	RequestCount   float64 `json:"request_count"`
	ThroughputRPM  float64 `json:"throughput_rpm"`
	HTTP4xxCount   float64 `json:"http_4xx_count"`
	HTTP5xxCount   float64 `json:"http_5xx_count"`
	GRPCErrorCount float64 `json:"grpc_error_count"`
}

type ServiceSummaryResult struct {
	Rows             []ServiceSummaryRow `json:"rows"`
	SortBy           string              `json:"sort_by"`
	Limit            int                 `json:"limit"`
	RowCount         int                 `json:"row_count"`
	MatchedCount     int                 `json:"matched_count"`
	Truncated        bool                `json:"truncated"`
	StartTime        string              `json:"start_time"`
	EndTime          string              `json:"end_time"`
	WindowMinutes    int                 `json:"window_minutes"`
	EnvScope         string              `json:"env_scope"`
	SortKeyUnit      string              `json:"sort_key_unit"`
	QueryFingerprint string              `json:"query_fingerprint"`
	Coverage         string              `json:"coverage"`
	Hint             string              `json:"hint,omitempty"`
}

type ServiceSummaryArgs struct {
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start of the interval in RFC3339/ISO8601 (e.g. 2024-06-01T12:00:00Z). When both start and end are set they beat lookback."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End of the interval in RFC3339/ISO8601 (e.g. 2024-06-01T13:00:00Z). When both start and end are set they beat lookback. A single bound fills the other with lookback_minutes."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Env             string  `json:"env,omitempty" jsonschema:"Environment PromQL regex (default: .*). Exact one-env match needs anchors (e.g. ^prod$). Invalid regex is rejected before querying."`
	SortBy          string  `json:"sort_by,omitempty" jsonschema:"Sort key. Allowed: request_count (default), throughput_rpm, http_4xx_count, http_5xx_count, grpc_error_count. throughput_rpm ranks identically to request_count. Unknown values including errors, error_rate, and 5xx are rejected."`
	Limit           int     `json:"limit,omitempty" jsonschema:"Max ranked rows. Omit or 0 means 10. Other values below 1 are an error. Values above 100 clamp to 100."`
}

type serviceSummaryClass struct {
	key          string
	extraMatcher string
	set          func(*ServiceSummaryRow, float64)
}

type serviceSummarySortSpec struct {
	key   string
	unit  string
	value func(ServiceSummaryRow) float64
}

var serviceSummaryQueried = []serviceSummaryClass{
	{serviceSummarySortRequest, "", func(r *ServiceSummaryRow, v float64) { r.RequestCount += v }},
	{serviceSummarySort4xx, `http_status_code=~"4.*"`, func(r *ServiceSummaryRow, v float64) { r.HTTP4xxCount += v }},
	{serviceSummarySort5xx, `http_status_code=~"5.*"`, func(r *ServiceSummaryRow, v float64) { r.HTTP5xxCount += v }},
	{serviceSummarySortGRPC, serviceSummaryGRPCMatcher, func(r *ServiceSummaryRow, v float64) { r.GRPCErrorCount += v }},
}

var serviceSummarySortSpecs = []serviceSummarySortSpec{
	{serviceSummarySortRequest, "count", func(r ServiceSummaryRow) float64 { return r.RequestCount }},
	{serviceSummarySortRPM, "rpm", func(r ServiceSummaryRow) float64 { return r.ThroughputRPM }},
	{serviceSummarySort4xx, "count", func(r ServiceSummaryRow) float64 { return r.HTTP4xxCount }},
	{serviceSummarySort5xx, "count", func(r ServiceSummaryRow) float64 { return r.HTTP5xxCount }},
	{serviceSummarySortGRPC, "count", func(r ServiceSummaryRow) float64 { return r.GRPCErrorCount }},
}

func NewServiceSummaryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceSummaryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ServiceSummaryArgs) (*mcp.CallToolResult, any, error) {
		sortSpec, err := resolveServiceSummarySortBy(args.SortBy)
		if err != nil {
			return nil, nil, err
		}
		limit, err := resolveServiceSummaryLimit(args.Limit)
		if err != nil {
			return nil, nil, err
		}

		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		envScope, envMatcher, err := serviceSummaryEnvMatcher(args.Env)
		if err != nil {
			return nil, nil, err
		}
		windowMin := intervalMinutes(startTimeParam, endTimeParam)
		// Stamp the interval that was actually summed (PromQL [Nm] rounds up),
		// not only the caller-requested bounds.
		queriedStart := endTimeParam - int64(windowMin)*60

		joined := map[string]*ServiceSummaryRow{}
		for _, class := range serviceSummaryQueried {
			series, err := fetchPromInstantSeries(ctx, client, cfg, class.key, class.query(envMatcher, windowMin), endTimeParam)
			if err != nil {
				return nil, nil, err
			}
			if err := mergeServiceSummarySeries(joined, series, class.set); err != nil {
				return nil, nil, err
			}
		}

		rows := make([]ServiceSummaryRow, 0, len(joined))
		for _, row := range joined {
			row.ThroughputRPM = row.RequestCount / float64(windowMin)
			rows = append(rows, *row)
		}

		sort.SliceStable(rows, func(i, j int) bool {
			vi := sortSpec.value(rows[i])
			vj := sortSpec.value(rows[j])
			if vi != vj {
				return vi > vj
			}
			return identityLess(rows[i].Service, rows[i].Env, rows[j].Service, rows[j].Env)
		})

		matchedCount := len(rows)
		truncated := matchedCount > limit
		if truncated {
			rows = rows[:limit]
		}
		for i := range rows {
			rows[i].Rank = i + 1
		}

		result := ServiceSummaryResult{
			Rows:             rows,
			SortBy:           sortSpec.key,
			Limit:            limit,
			RowCount:         len(rows),
			MatchedCount:     matchedCount,
			Truncated:        truncated,
			StartTime:        time.Unix(queriedStart, 0).UTC().Format(time.RFC3339),
			EndTime:          time.Unix(endTimeParam, 0).UTC().Format(time.RFC3339),
			WindowMinutes:    windowMin,
			EnvScope:         envScope,
			SortKeyUnit:      sortSpec.unit,
			QueryFingerprint: serviceSummaryFingerprint(windowMin),
			Coverage:         serviceSummaryCoverage,
		}
		if len(rows) == 0 {
			result.Hint = serviceSummaryEmptyHint
		}

		returnText, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		// Regex env → literal only for ^name$; BuildAPMServiceLink treats env as exact.
		dashboardURL := dlBuilder.BuildAPMServiceLink(queriedStart*1000, endTimeParam*1000, "", deeplink.APMCatalogEnvFromRegex(envScope), "")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(returnText),
				},
			},
		}, nil, nil
	}
}

func (c serviceSummaryClass) query(envMatcher string, windowMin int) string {
	return serviceSummaryCountQuery(envMatcher, windowMin, c.extraMatcher)
}

func resolveServiceSummarySortBy(sortBy string) (serviceSummarySortSpec, error) {
	if sortBy == "" {
		sortBy = serviceSummarySortRequest
	}
	for _, spec := range serviceSummarySortSpecs {
		if spec.key == sortBy {
			return spec, nil
		}
	}
	return serviceSummarySortSpec{}, fmt.Errorf("sort_by %q is not allowed; use one of: %s", sortBy, serviceSummaryAllowedSortBy())
}

func serviceSummaryAllowedSortBy() string {
	keys := make([]string, 0, len(serviceSummarySortSpecs))
	for _, spec := range serviceSummarySortSpecs {
		keys = append(keys, spec.key)
	}
	return strings.Join(keys, ", ")
}

func resolveServiceSummaryLimit(limit int) (int, error) {
	if limit == 0 {
		return serviceSummaryDefaultLimit, nil
	}
	if limit < 1 {
		return 0, fmt.Errorf("limit must be omitted, 0 (default %d), or >= 1; got %d", serviceSummaryDefaultLimit, limit)
	}
	if limit > serviceSummaryMaxLimit {
		return serviceSummaryMaxLimit, nil
	}
	return limit, nil
}

func serviceSummaryEnvMatcher(env string) (scope, matcher string, err error) {
	if env == "" {
		env = serviceSummaryDefaultEnv
	}
	if _, err := regexp.Compile(env); err != nil {
		return "", "", fmt.Errorf("env %q is not a valid regular expression: %w", env, err)
	}
	return env, fmt.Sprintf(`env=~"%s"`, escapePromQLLabel(env)), nil
}

func serviceSummaryCountQuery(envMatcher string, windowMin int, extraMatcher string) string {
	matchers := envMatcher + "," + serviceSummarySpanKind
	if extraMatcher != "" {
		matchers += "," + extraMatcher
	}
	return fmt.Sprintf("sum by (service_name, env)(sum_over_time(trace_endpoint_count{%s}[%dm]))", matchers, windowMin)
}

func serviceSummaryFingerprint(windowMin int) string {
	envMatcher := fmt.Sprintf(`env=~"%s"`, serviceSummaryFingerprintEnv)
	parts := make([]string, 0, len(serviceSummaryQueried))
	for _, class := range serviceSummaryQueried {
		parts = append(parts, class.query(envMatcher, windowMin))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func intervalMinutes(startUnix, endUnix int64) int {
	start := time.Unix(startUnix, 0).UTC()
	end := time.Unix(endUnix, 0).UTC()
	d := end.Sub(start)
	minutes := int(d.Minutes())
	if d%time.Minute != 0 {
		minutes++
	}
	if minutes <= 0 {
		minutes = 1
	}
	return minutes
}

func fetchPromInstantSeries(ctx context.Context, client *http.Client, cfg models.Config, classKey, query string, endTime int64) (apiPromInstantResp, error) {
	httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, query, endTime, cfg)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, promErr(httpResp, "service summary "+classKey)
	}
	var parsed apiPromInstantResp
	if err := json.NewDecoder(httpResp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode service summary %s: %w", classKey, err)
	}
	return parsed, nil
}

func mergeServiceSummarySeries(joined map[string]*ServiceSummaryRow, series apiPromInstantResp, set func(*ServiceSummaryRow, float64)) error {
	for _, r := range series {
		serviceName := r.Metric["service_name"]
		if serviceName == "" {
			continue
		}
		val, err := parsePromInstantValue(r.Value)
		if err != nil {
			return err
		}
		if math.IsNaN(val) || math.IsInf(val, 0) {
			// Non-finite samples are not rankable; skip rather than failing the
			// whole fleet response or poisoning sort order.
			continue
		}
		env := r.Metric["env"]
		key := serviceName + "\x00" + env
		row, ok := joined[key]
		if !ok {
			row = &ServiceSummaryRow{Service: serviceName, Env: env}
			joined[key] = row
		}
		set(row, val)
	}
	return nil
}

func parsePromInstantValue(value []any) (float64, error) {
	if len(value) < 2 {
		return 0, fmt.Errorf("prometheus instant sample is missing a value")
	}
	switch v := value[1].(type) {
	case string:
		val, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("prometheus instant sample value %q is not a number: %w", v, err)
		}
		return val, nil
	case float64:
		return v, nil
	default:
		return 0, fmt.Errorf("prometheus instant sample value is not a string or number")
	}
}
