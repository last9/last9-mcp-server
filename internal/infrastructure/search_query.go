package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"last9-mcp/internal/utils"
)

type instantPoint struct {
	Metric map[string]string `json:"metric"`
}

func fetchSearchMetrics(ctx context.Context, q searchQuery, ts int64) ([]map[string]string, bool, error) {
	promql := searchPromQL(q.args.EntityType, q.args.Query)
	resp, err := utils.MakePromInstantAPIQuery(ctx, q.client, promql, ts, q.cfg)
	if err != nil {
		return nil, false, fmt.Errorf("infrastructure search query failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPISuccessBodyBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read search response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, false, fmt.Errorf("infrastructure search returned status %d: %s", resp.StatusCode, truncateAPIError(body, resp.StatusCode))
	}
	if int64(len(body)) > maxAPISuccessBodyBytes {
		return nil, false, fmt.Errorf("infrastructure search response exceeds %d bytes", maxAPISuccessBodyBytes)
	}
	return parseInstantMetrics(body)
}

func parseInstantMetrics(body []byte) ([]map[string]string, bool, error) {
	var points []instantPoint
	if err := json.Unmarshal(body, &points); err != nil {
		return nil, false, fmt.Errorf("failed to parse search response: %w", err)
	}
	truncated := len(points) > maxSearchFetch
	if truncated {
		points = points[:maxSearchFetch]
	}
	out := make([]map[string]string, 0, len(points))
	for _, point := range points {
		if len(point.Metric) == 0 {
			continue
		}
		out = append(out, point.Metric)
	}
	return out, truncated, nil
}

func searchPromQL(entityType, query string) string {
	switch entityType {
	case "host":
		return wrapMetric(`{__name__=~"node_uname_info|system_uname_info"}`, optionalRegexMatcher("nodename", query))
	case "k8s_cluster":
		return wrapMetric("kube_node_info", optionalRegexMatcher("cluster", query))
	case "k8s_node":
		return wrapMetric("kube_node_info", optionalRegexMatcher("node", query))
	default:
		return wrapMetric("kube_pod_info", optionalRegexMatcher("pod", query))
	}
}

func wrapMetric(metric, matcher string) string {
	if matcher == "" {
		return metric
	}
	if strings.HasPrefix(metric, "{") {
		inner := strings.TrimSuffix(strings.TrimPrefix(metric, "{"), "}")
		return "{" + inner + "," + matcher + "}"
	}
	return metric + "{" + matcher + "}"
}

func optionalRegexMatcher(label, query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	return fmt.Sprintf(`%s=~".*%s.*"`, label, regexp.QuoteMeta(q))
}
