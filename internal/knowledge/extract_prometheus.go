package knowledge

import (
	"fmt"
	"time"
)

// PrometheusExtractor handles output of prometheus_instant_query and
// prometheus_range_query. Extracts topology (nodes + edges) from entity-
// identifying labels and attaches statistics to the most specific entity.
//
// Instant query shape:
//
//	[{"metric": {"__name__": "...", ...}, "value": [timestamp, "val"]}]
//
// Range query shape:
//
//	[{"metric": {"__name__": "...", ...}, "values": [[ts, val], ...]}]
type PrometheusExtractor struct{}

func (e *PrometheusExtractor) Name() string { return "prometheus" }

// CanHandle returns true when input is an array where elements have a "metric" map
// AND either "value" or "values".
func (e *PrometheusExtractor) CanHandle(parsed interface{}) bool {
	arr, ok := parsed.([]interface{})
	if !ok || len(arr) == 0 {
		return false
	}

	// Check first element for the prometheus result shape
	first, ok := arr[0].(map[string]interface{})
	if !ok {
		return false
	}

	_, hasMetric := first["metric"]
	_, hasValue := first["value"]
	_, hasValues := first["values"]

	return hasMetric && (hasValue || hasValues)
}

func (e *PrometheusExtractor) Extract(parsed interface{}) (*ExtractionResult, error) {
	arr := parsed.([]interface{})

	result := &ExtractionResult{
		Confidence: 0.8,
	}
	now := time.Now()

	// Dedup sets keyed by node ID and edge signature.
	nodeSet := make(map[string]Node)
	edgeSet := make(map[string]Edge)

	for _, elemRaw := range arr {
		elem, ok := elemRaw.(map[string]interface{})
		if !ok {
			continue
		}

		metricLabels, ok := elem["metric"].(map[string]interface{})
		if !ok {
			continue
		}

		metricName := "unknown"
		if name, ok := metricLabels["__name__"].(string); ok {
			metricName = name
		}

		// Phase 1: resolve entities from labels.
		entities := resolveEntities(metricLabels, metricName)

		// Phase 2: add unique nodes.
		for _, ent := range entities {
			if _, exists := nodeSet[ent.nodeID]; !exists {
				nodeSet[ent.nodeID] = Node{
					ID:   ent.nodeID,
					Type: ent.nodeType,
					Name: ent.name,
				}
			}
		}

		// Phase 3: infer edges from co-occurrence.
		for _, edge := range inferEdges(entities) {
			key := edge.SourceID + "|" + edge.Relation + "|" + edge.TargetID
			if _, exists := edgeSet[key]; !exists {
				edgeSet[key] = edge
			}
		}

		// Determine stat target: highest priority entity, or legacy fallback.
		statNodeID := pickStatTarget(entities)
		if statNodeID == "" {
			statNodeID = resolvePrometheusNodeID(metricLabels)
		}

		// Qualify stat name and extract unit.
		qualName, unit := qualifyStatName(metricName, metricLabels)

		// Extract value — instant query.
		if valueArr, ok := elem["value"].([]interface{}); ok && len(valueArr) >= 2 {
			val := parsePrometheusValue(valueArr[1])
			result.Stats = append(result.Stats, Statistic{
				NodeID:     statNodeID,
				MetricName: qualName,
				Value:      val,
				Unit:       unit,
				Timestamp:  now,
			})
			continue
		}

		// Extract values — range query: use the most recent value.
		if valuesArr, ok := elem["values"].([]interface{}); ok && len(valuesArr) > 0 {
			last, ok := valuesArr[len(valuesArr)-1].([]interface{})
			if ok && len(last) >= 2 {
				val := parsePrometheusValue(last[1])
				result.Stats = append(result.Stats, Statistic{
					NodeID:     statNodeID,
					MetricName: qualName,
					Value:      val,
					Unit:       unit,
					Timestamp:  now,
				})
			}
		}
	}

	// Flatten dedup sets into result slices.
	for _, n := range nodeSet {
		result.Nodes = append(result.Nodes, n)
	}
	for _, e := range edgeSet {
		result.Edges = append(result.Edges, e)
	}

	if len(result.Nodes) == 0 && len(result.Stats) == 0 {
		result.Confidence = 0.2
	}

	return result, nil
}

func resolvePrometheusNodeID(labels map[string]interface{}) string {
	// Priority order for node identification
	for _, key := range []string{"service_name", "service", "job", "instance"} {
		if val, ok := labels[key].(string); ok && val != "" {
			return MakeNodeID("Service", val)
		}
	}
	// Fallback: use metric name
	if name, ok := labels["__name__"].(string); ok {
		return "metric:" + name
	}
	return "metric:unknown"
}

// parsePrometheusValue handles both string and numeric prometheus values.
// Prometheus instant queries return values as strings: [timestamp, "1.23"]
func parsePrometheusValue(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}
