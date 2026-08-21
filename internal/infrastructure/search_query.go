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

const maxSearchBodyBytes = 4 * 1024 * 1024

type instantPoint struct {
	Metric map[string]string `json:"metric"`
}

func fetchSearchMetrics(ctx context.Context, q searchQuery, ts int64) ([]map[string]string, bool, error) {
	promql, err := searchPromQL(q.args.EntityType, q.args.Query)
	if err != nil {
		return nil, false, err
	}
	resp, err := utils.MakePromInstantAPIQuery(ctx, q.client, promql, ts, q.cfg)
	if err != nil {
		return nil, false, fmt.Errorf("infrastructure search query failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBodyBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read search response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, false, fmt.Errorf("infrastructure search returned status %d: %s", resp.StatusCode, truncateAPIError(body, resp.StatusCode))
	}
	if int64(len(body)) > maxSearchBodyBytes {
		return nil, false, fmt.Errorf("infrastructure search response exceeds %d bytes", maxSearchBodyBytes)
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

func searchPromQL(entityType, query string) (string, error) {
	matcher, err := searchMatcher(entityType, query)
	if err != nil {
		return "", err
	}
	inner := wrapMetric(searchMetric(entityType), matcher)
	return "count by (" + searchByLabels(entityType) + ") (" + inner + ")", nil
}

func searchMetric(entityType string) string {
	switch entityType {
	case "host":
		return `{__name__=~"node_uname_info|system_uname_info"}`
	case "k8s_pod":
		return "kube_pod_info"
	default:
		return "kube_node_info"
	}
}

func searchByLabels(entityType string) string {
	switch entityType {
	case "host":
		return "instance_id,nodename,instance_name,instance,job,host_name"
	case "k8s_cluster":
		return "cluster"
	case "k8s_node":
		return "cluster,node"
	default:
		return "cluster,namespace,pod,uid,node"
	}
}

func searchMatcher(entityType, query string) (string, error) {
	// Host names live on several labels; Prom-side filter would miss instance_id /
	// host_name. List all hosts and substring-filter in Go.
	if entityType == "host" {
		return "", nil
	}
	label := "node"
	if entityType == "k8s_cluster" {
		label = "cluster"
	}
	if entityType == "k8s_pod" {
		label = "pod"
	}
	return optionalRegexMatcher(label, query)
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

func optionalRegexMatcher(label, query string) (string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", nil
	}
	escaped, err := escapePromRegex(q)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`%s=~".*%s.*"`, label, escaped), nil
}

func escapePromRegex(value string) (string, error) {
	if strings.ContainsAny(value, "\n\r}") {
		return "", fmt.Errorf("query contains invalid characters")
	}
	quoted := regexp.QuoteMeta(value)
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(quoted), nil
}
