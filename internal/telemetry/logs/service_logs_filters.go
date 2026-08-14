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

func compileServiceLogsStructuredFilters(ctx context.Context, client *http.Client, cfg models.Config, args GetServiceLogsArgs, startTime, endTime time.Time, index string) ([]map[string]interface{}, []map[string]interface{}, error) {
	extra := make([]map[string]interface{}, 0, len(args.AttributeFilters)+1)
	for i, filter := range args.AttributeFilters {
		field, err := compileServiceLogAttributeField(filter.Field, fmt.Sprintf("attribute_filters[%d].field", i))
		if err != nil {
			return nil, nil, err
		}
		extra = append(extra, map[string]interface{}{
			"$eq": []interface{}{field, filter.Value},
		})
	}

	class := strings.TrimSpace(args.HTTPStatusClass)
	code := strings.TrimSpace(args.HTTPStatusCode)
	if class == "" && code == "" {
		if strings.TrimSpace(args.HTTPStatusField) != "" {
			return nil, nil, fmt.Errorf("http_status_field requires http_status_class or http_status_code")
		}
		return extra, nil, nil
	}

	if class != "" && !httpStatusClassPattern.MatchString(class) {
		return nil, nil, fmt.Errorf("invalid http_status_class %q: use 2xx, 3xx, 4xx, or 5xx", class)
	}
	if code != "" && !httpStatusCodePattern.MatchString(code) {
		return nil, nil, fmt.Errorf("invalid http_status_code %q: use a 3-digit HTTP status such as 500 or 401", code)
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
			return nil, nil, err
		}
	} else {
		statusField, parseStages, err = resolveHTTPStatusField(ctx, client, cfg, args, startTime, endTime, index)
		if err != nil {
			return nil, nil, err
		}
	}

	extra = append(extra, httpStatusCondition(statusField, class, code))
	return extra, parseStages, nil
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

func httpStatusCondition(field, class, code string) map[string]interface{} {
	if httpStatusCodePattern.MatchString(code) {
		return map[string]interface{}{
			"$eq": []interface{}{field, code},
		}
	}
	digit := httpStatusClassPattern.FindStringSubmatch(class)[1]
	return map[string]interface{}{
		"$regex": []interface{}{field, "^" + digit},
	}
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
	needles := []string{
		"http.response.status_code",
		"http_response_status_code",
		"http.status_code",
		"http_status_code",
		"http.status",
		"http_status",
		"status_code",
		"statuscode",
	}
	for _, needle := range needles {
		if n == needle || strings.Contains(n, needle) || strings.Contains(f, needle) {
			return true
		}
	}
	return false
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

func applyServiceLogsStructuredFilters(query []map[string]interface{}, extra []map[string]interface{}, parseStages []map[string]interface{}) []map[string]interface{} {
	if len(extra) == 0 && len(parseStages) == 0 {
		return query
	}

	if len(parseStages) == 0 {
		if len(query) == 0 {
			return query
		}
		filterStage := mapsClone(query[0])
		queryMap, ok := filterStage["query"].(map[string]interface{})
		if !ok {
			return query
		}
		clonedQuery := mapsClone(queryMap)
		andConditions, ok := clonedQuery["$and"].([]interface{})
		if !ok {
			return query
		}
		clonedConditions := append([]interface{}(nil), andConditions...)
		for _, cond := range extra {
			clonedConditions = append(clonedConditions, cond)
		}
		clonedQuery["$and"] = clonedConditions
		filterStage["query"] = clonedQuery
		query[0] = filterStage
		return query
	}

	out := append([]map[string]interface{}{}, query...)
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
	return out
}
