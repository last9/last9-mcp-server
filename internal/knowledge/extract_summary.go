package knowledge

import (
	"time"
)

// ServiceSummaryExtractor handles output of get_service_summary.
//
// Expected shape:
//
//	{
//	  "svc-name": {"ServiceName": "...", "Throughput": 123, "ErrorRate": 0, "ResponseTime": 5.2},
//	  "svc-name2": {...}
//	}
//
// All top-level values are objects containing "ServiceName" or "Throughput".
type ServiceSummaryExtractor struct{}

func (e *ServiceSummaryExtractor) Name() string { return "service_summary" }

// CanHandle returns true when the top-level object has all map values containing
// "ServiceName" or "Throughput" fields. Must have at least one entry.
func (e *ServiceSummaryExtractor) CanHandle(parsed interface{}) bool {
	obj, ok := parsed.(map[string]interface{})
	if !ok || len(obj) == 0 {
		return false
	}

	matchCount := 0
	for _, val := range obj {
		inner, ok := val.(map[string]interface{})
		if !ok {
			return false // all values must be objects
		}
		_, hasSN := inner["ServiceName"]
		_, hasThroughput := inner["Throughput"]
		if hasSN || hasThroughput {
			matchCount++
		}
	}
	return matchCount == len(obj)
}

func (e *ServiceSummaryExtractor) Extract(parsed interface{}) (*ExtractionResult, error) {
	obj := parsed.(map[string]interface{})

	result := &ExtractionResult{
		Confidence: 0.85,
	}
	now := time.Now()

	for key, val := range obj {
		inner, ok := val.(map[string]interface{})
		if !ok {
			continue
		}

		// Use ServiceName if present, otherwise use the map key
		svcName, _ := inner["ServiceName"].(string)
		if svcName == "" {
			svcName = key
		}

		nodeID := MakeNodeID("Service", svcName)
		result.Nodes = append(result.Nodes, Node{
			ID:   nodeID,
			Type: "Service",
			Name: svcName,
		})

		// Extract metrics as statistics
		metricFields := []struct {
			key  string
			unit string
		}{
			{"Throughput", "req/s"},
			{"ErrorRate", "errors/s"},
			{"ResponseTime", "ms"},
		}

		for _, mf := range metricFields {
			if val, ok := toFloat64(inner[mf.key]); ok {
				result.Stats = append(result.Stats, Statistic{
					NodeID:     nodeID,
					MetricName: mf.key,
					Value:      val,
					Unit:       mf.unit,
					Timestamp:  now,
				})
			}
		}
	}

	if len(result.Nodes) == 0 {
		result.Confidence = 0.2
	}

	return result, nil
}
