package knowledge

import (
	"time"
)

// OperationsSummaryExtractor handles output of get_service_operations_summary.
//
// Expected shape:
//
//	{
//	  "service_name": "X",
//	  "operations": [
//	    {"name": "GET /api", "db_system": "mysql", "net_peer_name": "host", ...}
//	  ]
//	}
type OperationsSummaryExtractor struct{}

func (e *OperationsSummaryExtractor) Name() string { return "operations_summary" }

// CanHandle checks for the distinctive shape: top-level object with "service_name" (string)
// AND "operations" (array). Distinguished from dependency graph by having "operations"
// instead of "incoming"/"outgoing"/"databases".
func (e *OperationsSummaryExtractor) CanHandle(parsed interface{}) bool {
	obj, ok := parsed.(map[string]interface{})
	if !ok {
		return false
	}

	svcName, hasSvcName := obj["service_name"]
	ops, hasOps := obj["operations"]
	if !hasSvcName || !hasOps {
		return false
	}

	_, svcIsStr := svcName.(string)
	_, opsIsArr := ops.([]interface{})
	return svcIsStr && opsIsArr
}

func (e *OperationsSummaryExtractor) Extract(parsed interface{}) (*ExtractionResult, error) {
	obj := parsed.(map[string]interface{})

	serviceName, _ := obj["service_name"].(string)
	if serviceName == "" {
		return &ExtractionResult{Confidence: 0.1}, nil
	}

	result := &ExtractionResult{
		Confidence: 0.9,
	}
	now := time.Now()

	// Root service node
	svcID := MakeNodeID("Service", serviceName)
	result.Nodes = append(result.Nodes, Node{
		ID:   svcID,
		Type: "Service",
		Name: serviceName,
	})

	operations, _ := obj["operations"].([]interface{})
	for _, opRaw := range operations {
		op, ok := opRaw.(map[string]interface{})
		if !ok {
			continue
		}

		opName, _ := op["name"].(string)
		if opName == "" {
			continue
		}

		// HTTPEndpoint per operation
		endpointID := MakeNodeID("HTTPEndpoint", serviceName, opName)
		result.Nodes = append(result.Nodes, Node{
			ID:   endpointID,
			Type: "HTTPEndpoint",
			Name: opName,
		})
		result.Edges = append(result.Edges, Edge{
			SourceID: svcID,
			TargetID: endpointID,
			Relation: "EXPOSES",
		})

		// If db_system is present, create DataStoreInstance + CONNECTS_TO
		dbSystem, _ := op["db_system"].(string)
		if dbSystem != "" {
			netPeer, _ := op["net_peer_name"].(string)
			var dbID string
			if netPeer != "" {
				dbID = MakeNodeID("DataStoreInstance", dbSystem, netPeer)
			} else {
				dbID = MakeNodeID("DataStoreInstance", dbSystem)
			}
			result.Nodes = append(result.Nodes, Node{
				ID:   dbID,
				Type: "DataStoreInstance",
				Name: dbSystem,
				Properties: map[string]interface{}{
					"db_system":     dbSystem,
					"net_peer_name": netPeer,
				},
			})
			result.Edges = append(result.Edges, Edge{
				SourceID: endpointID,
				TargetID: dbID,
				Relation: "CONNECTS_TO",
			})
		}

		// If messaging_system is present, create messaging node
		msgSystem, _ := op["messaging_system"].(string)
		if msgSystem != "" {
			msgID := MakeNodeID("KafkaTopic", msgSystem)
			result.Nodes = append(result.Nodes, Node{
				ID:   msgID,
				Type: "KafkaTopic",
				Name: msgSystem,
			})
			result.Edges = append(result.Edges, Edge{
				SourceID: endpointID,
				TargetID: msgID,
				Relation: "PRODUCES_TO",
			})
		}

		// Extract metrics as statistics on the endpoint
		extractOperationStats(result, endpointID, op, now)
	}

	if len(result.Nodes) <= 1 {
		result.Confidence = 0.3
	}

	return result, nil
}

func extractOperationStats(result *ExtractionResult, nodeID string, op map[string]interface{}, now time.Time) {
	simpleMetrics := []struct {
		key  string
		unit string
	}{
		{"throughput", "req/s"},
		{"error_rate", "errors/s"},
		{"error_percent", "%"},
	}

	for _, mf := range simpleMetrics {
		if val, ok := toFloat64(op[mf.key]); ok && val != 0 {
			result.Stats = append(result.Stats, Statistic{
				NodeID:     nodeID,
				MetricName: mf.key,
				Value:      val,
				Unit:       mf.unit,
				Timestamp:  now,
			})
		}
	}

	// response_time is a nested map: {"p50": 25.5, "p95": 45.2, ...}
	if rt, ok := op["response_time"].(map[string]interface{}); ok {
		for percentile, val := range rt {
			if fv, ok := toFloat64(val); ok && fv != 0 {
				result.Stats = append(result.Stats, Statistic{
					NodeID:     nodeID,
					MetricName: "response_time." + percentile,
					Value:      fv,
					Unit:       "ms",
					Timestamp:  now,
				})
			}
		}
	}
}
