package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

var (
	httpStatusClassPattern = regexp.MustCompile(`(?i)^([1-5])xx$`)
	httpStatusCodePattern  = regexp.MustCompile(`^[1-5][0-9]{2}$`)
)

func compileServiceLogsStructuredFilters(ctx context.Context, client *http.Client, cfg models.Config, args GetServiceLogsArgs, startTime, endTime time.Time, index string) ([]map[string]interface{}, []map[string]interface{}, string, error) {
	extra := make([]map[string]interface{}, 0, len(args.AttributeFilters)+1)
	for i, filter := range args.AttributeFilters {
		field, err := compileServiceLogAttributeField(filter.Field, fmt.Sprintf("attribute_filters[%d].field", i))
		if err != nil {
			return nil, nil, "", err
		}
		extra = append(extra, map[string]interface{}{
			"$eq": []interface{}{field, filter.Value},
		})
	}

	class := strings.TrimSpace(args.HTTPStatusClass)
	code := strings.TrimSpace(args.HTTPStatusCode)
	if class == "" && code == "" {
		if strings.TrimSpace(args.HTTPStatusField) != "" {
			return nil, nil, "", fmt.Errorf("http_status_field requires http_status_class or http_status_code")
		}
		return extra, nil, "", nil
	}

	if class != "" && !httpStatusClassPattern.MatchString(class) {
		return nil, nil, "", fmt.Errorf("invalid http_status_class %q: use 2xx, 3xx, 4xx, or 5xx", class)
	}
	if code != "" && !httpStatusCodePattern.MatchString(code) {
		return nil, nil, "", fmt.Errorf("invalid http_status_code %q: use a 3-digit HTTP status such as 500 or 401", code)
	}

	explicitField := strings.TrimSpace(args.HTTPStatusField)
	var (
		statusField string
		parseStages []map[string]interface{}
		err         error
	)
	if explicitField != "" {
		statusField, err = compileServiceLogAttributeField(explicitField, "http_status_field")
		if err != nil {
			return nil, nil, "", err
		}
	} else {
		statusField, parseStages, err = resolveHTTPStatusField(ctx, client, cfg, args, startTime, endTime, index)
		if err != nil {
			return nil, nil, "", err
		}
	}

	cond, err := httpStatusCondition(statusField, class, code)
	if err != nil {
		return nil, nil, "", err
	}
	extra = append(extra, cond)
	return extra, parseStages, statusField, nil
}

func compileServiceLogAttributeField(field, path string) (string, error) {
	trimmed := strings.TrimSpace(field)
	// get_service_logs accepts a short attribute name and wraps it; fail-closed
	// sanitize then checks the bracket form. Leave resource_ / trace-only / quoted
	// syntax for sanitize to reject with its existing tips.
	if logSimpleFieldRefPattern.MatchString(trimmed) &&
		!isCanonicalLogFieldRef(trimmed) &&
		!isTraceOnlyLogField(trimmed) &&
		!strings.HasPrefix(trimmed, "resource_") {
		trimmed = fmt.Sprintf("attributes['%s']", trimmed)
	}
	next, err := sanitizeLogFieldRef(trimmed, path)
	if err != nil {
		return "", err
	}
	if next == "" {
		return "", fmt.Errorf("invalid log field reference %q at %s: use ServiceName, attributes['field'], or resources['field']", field, path)
	}
	if isCanonicalLogFieldRef(next) {
		return next, nil
	}
	return "", fmt.Errorf("invalid log field reference %q at %s: use ServiceName, attributes['field'], or resources['field']", field, path)
}

func httpStatusCondition(field, class, code string) (map[string]interface{}, error) {
	if httpStatusCodePattern.MatchString(code) {
		return map[string]interface{}{
			"$eq": []interface{}{field, code},
		}, nil
	}
	parts := httpStatusClassPattern.FindStringSubmatch(class)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid http_status_class %q: use 2xx, 3xx, 4xx, or 5xx", class)
	}
	return map[string]interface{}{
		"$regex": []interface{}{field, "^" + parts[1]},
	}, nil
}

func resolveHTTPStatusField(ctx context.Context, client *http.Client, cfg models.Config, args GetServiceLogsArgs, startTime, endTime time.Time, index string) (string, []map[string]interface{}, error) {
	pipeline := buildServiceLogsQuery(args.ServiceName, nil, nil)
	if args.Env != "" {
		pipeline = addServiceLogsEnvFilter(pipeline, args.Env)
	}
	validatedPipeline, err := prepareLogJSONQuery(pipeline, "pipeline")
	if err != nil {
		return "", nil, err
	}

	startSec := startTime.Unix()
	endSec := endTime.Unix()
	maxWindowSeconds := int64(utils.MaxLogAttributeLookbackMinutes * 60)
	if endSec-startSec > maxWindowSeconds {
		startSec = endSec - maxWindowSeconds
	}

	queryParams := url.Values{}
	queryParams.Set("region", cfg.Region)
	queryParams.Set("start", fmt.Sprintf("%d", startSec))
	queryParams.Set("end", fmt.Sprintf("%d", endSec))
	if index != "" {
		queryParams.Set("index", index)
	}

	attrs, err := discoverLogAttributes(ctx, client, cfg, validatedPipeline, startSec, endSec, index, queryParams)
	if err != nil {
		return "", nil, fmt.Errorf("failed to discover HTTP status field: %w", err)
	}

	matches := make([]LogAttribute, 0)
	for _, attr := range attrs {
		if isHTTPStatusLikeAttribute(attr) {
			matches = append(matches, attr)
		}
	}

	if len(matches) == 0 {
		return "", nil, fmt.Errorf("no HTTP status field found for this service/env/window; pass http_status_field with the logjson field to use (e.g. attributes['http.status_code'])")
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, attr := range matches {
			names = append(names, attr.FilterField)
		}
		return "", nil, fmt.Errorf("multiple HTTP status fields found (%s); pass http_status_field with the filter_field to use", strings.Join(names, ", "))
	}

	return matches[0].FilterField, parseStagesFromAttributeHint(matches[0]), nil
}

func isHTTPStatusLikeAttribute(attr LogAttribute) bool {
	n := strings.ToLower(attr.Name)
	f := strings.ToLower(attr.FilterField)
	combined := n + " " + f
	if strings.Contains(combined, "grpc") {
		return false
	}
	if strings.Contains(n, "http") && strings.Contains(n, "status") {
		return true
	}
	if strings.Contains(f, "http") && strings.Contains(f, "status") {
		return true
	}
	base := httpStatusAttributeBase(attr.Name)
	if base == "" {
		base = httpStatusAttributeBase(attr.FilterField)
	}
	switch base {
	case "status_code", "statuscode", "status":
		return true
	default:
		return false
	}
}

func httpStatusAttributeBase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "attributes['")
	s = strings.TrimPrefix(s, "resources['")
	s = strings.TrimSuffix(s, "']")
	if i := strings.LastIndexAny(s, "./"); i >= 0 {
		s = s[i+1:]
	}
	return strings.ReplaceAll(s, "-", "_")
}

func parseStagesFromAttributeHint(attr LogAttribute) []map[string]interface{} {
	if attr.Source != "body" || strings.TrimSpace(attr.Hint) == "" {
		return nil
	}
	var stages []map[string]interface{}
	if err := json.Unmarshal([]byte(attr.Hint), &stages); err != nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, 1)
	for _, stage := range stages {
		if stageType, _ := stage["type"].(string); stageType == "parse" {
			out = append(out, stage)
		}
	}
	return out
}

func applyServiceLogsStructuredFilters(query []map[string]interface{}, extra []map[string]interface{}, parseStages []map[string]interface{}) ([]map[string]interface{}, error) {
	if len(extra) == 0 && len(parseStages) == 0 {
		return query, nil
	}
	if len(query) == 0 {
		return nil, fmt.Errorf("internal error: service log query is missing the service filter; cannot apply status/attribute filters")
	}

	out := make([]map[string]interface{}, len(query))
	for i, stage := range query {
		out[i] = mapsClone(stage)
	}

	if len(parseStages) == 0 {
		filterStage := mapsClone(out[0])
		queryMap, ok := filterStage["query"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("internal error: service log query filter is missing query; cannot apply status/attribute filters")
		}
		clonedQuery := mapsClone(queryMap)
		andConditions, ok := clonedQuery["$and"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("internal error: service log query filter is missing $and; cannot apply status/attribute filters")
		}
		clonedConditions := append([]interface{}(nil), andConditions...)
		for _, cond := range extra {
			clonedConditions = append(clonedConditions, cond)
		}
		clonedQuery["$and"] = clonedConditions
		filterStage["query"] = clonedQuery
		out[0] = filterStage
		return out, nil
	}

	out = append(out, parseStages...)
	if len(extra) > 0 {
		conds := make([]interface{}, 0, len(extra))
		for _, cond := range extra {
			conds = append(conds, cond)
		}
		out = append(out, map[string]interface{}{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": conds,
			},
		})
	}
	return out, nil
}
