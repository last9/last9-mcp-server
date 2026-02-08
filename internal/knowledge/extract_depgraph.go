package knowledge

import (
	"time"
)

// DependencyGraphExtractor handles output of get_service_dependency_graph.
//
// Expected shape:
//
//	{
//	  "service_name": "X",
//	  "incoming": {"svc": {metrics}},
//	  "outgoing": {"svc": {metrics}},
//	  "databases": {"db": {metrics}},
//	  "messaging_systems": {"msg": {metrics}}
//	}
type DependencyGraphExtractor struct{}

func (e *DependencyGraphExtractor) Name() string { return "dependency_graph" }

// CanHandle checks for the distinctive shape: top-level object with "service_name"
// AND at least one of "incoming", "outgoing", or "databases".
func (e *DependencyGraphExtractor) CanHandle(parsed interface{}) bool {
	obj, ok := parsed.(map[string]interface{})
	if !ok {
		return false
	}

	_, hasServiceName := obj["service_name"]
	if !hasServiceName {
		return false
	}

	_, hasIncoming := obj["incoming"]
	_, hasOutgoing := obj["outgoing"]
	_, hasDatabases := obj["databases"]
	return hasIncoming || hasOutgoing || hasDatabases
}

func (e *DependencyGraphExtractor) Extract(parsed interface{}) (*ExtractionResult, error) {
	obj := parsed.(map[string]interface{})

	serviceName, _ := obj["service_name"].(string)
	if serviceName == "" {
		return &ExtractionResult{Confidence: 0.1}, nil
	}

	// Extract env from top-level field when available
	env, _ := obj["env"].(string)

	result := &ExtractionResult{
		Confidence: 0.9,
	}
	now := time.Now()

	// Root service node
	rootID := MakeNodeID("Service", serviceName)
	result.Nodes = append(result.Nodes, Node{
		ID:   rootID,
		Type: "Service",
		Name: serviceName,
		Env:  env,
	})

	// Incoming services: they CALL us
	if incoming, ok := obj["incoming"].(map[string]interface{}); ok {
		for svcName, metricsRaw := range incoming {
			svcID := MakeNodeID("Service", svcName)
			result.Nodes = append(result.Nodes, Node{
				ID:   svcID,
				Type: "Service",
				Name: svcName,
				Env:  env,
			})
			result.Edges = append(result.Edges, Edge{
				SourceID: svcID,
				TargetID: rootID,
				Relation: "CALLS",
			})
			appendRedMetricStats(result, svcID, svcName+"->"+serviceName, metricsRaw, now)
		}
	}

	// Outgoing services: we CALL them
	if outgoing, ok := obj["outgoing"].(map[string]interface{}); ok {
		for svcName, metricsRaw := range outgoing {
			svcID := MakeNodeID("Service", svcName)
			result.Nodes = append(result.Nodes, Node{
				ID:   svcID,
				Type: "Service",
				Name: svcName,
				Env:  env,
			})
			result.Edges = append(result.Edges, Edge{
				SourceID: rootID,
				TargetID: svcID,
				Relation: "CALLS",
			})
			appendRedMetricStats(result, svcID, serviceName+"->"+svcName, metricsRaw, now)
		}
	}

	// Databases: we CONNECT_TO them
	if databases, ok := obj["databases"].(map[string]interface{}); ok {
		for dbName, metricsRaw := range databases {
			dbID := MakeNodeID("DataStoreInstance", dbName)
			result.Nodes = append(result.Nodes, Node{
				ID:   dbID,
				Type: "DataStoreInstance",
				Name: dbName,
				Env:  env,
			})
			result.Edges = append(result.Edges, Edge{
				SourceID: rootID,
				TargetID: dbID,
				Relation: "CONNECTS_TO",
			})
			appendRedMetricStats(result, dbID, serviceName+"->"+dbName, metricsRaw, now)
		}
	}

	// Messaging systems: we produce/consume
	if messaging, ok := obj["messaging_systems"].(map[string]interface{}); ok {
		for msgName, metricsRaw := range messaging {
			msgID := MakeNodeID("KafkaTopic", msgName)
			result.Nodes = append(result.Nodes, Node{
				ID:   msgID,
				Type: "KafkaTopic",
				Name: msgName,
				Env:  env,
			})
			result.Edges = append(result.Edges, Edge{
				SourceID: rootID,
				TargetID: msgID,
				Relation: "PRODUCES_TO",
			})
			appendRedMetricStats(result, msgID, serviceName+"->"+msgName, metricsRaw, now)
		}
	}

	return result, nil
}

// appendRedMetricStats extracts RED metrics (Rate, Errors, Duration) from a
// metrics map and adds them as Statistics on the given edge label.
func appendRedMetricStats(result *ExtractionResult, nodeID, edgeLabel string, metricsRaw interface{}, now time.Time) {
	metrics, ok := metricsRaw.(map[string]interface{})
	if !ok {
		return
	}

	metricFields := []struct {
		key  string
		unit string
	}{
		{"Throughput", "req/s"},
		{"ResponseTimeP95", "ms"},
		{"ResponseTimeP50", "ms"},
		{"ResponseTimeP90", "ms"},
		{"ResponseTimeAvg", "ms"},
		{"ErrorRate", "errors/s"},
		{"ErrorPercent", "%"},
	}

	for _, mf := range metricFields {
		if val, ok := toFloat64(metrics[mf.key]); ok && val != 0 {
			result.Stats = append(result.Stats, Statistic{
				NodeID:     nodeID,
				MetricName: edgeLabel + "." + mf.key,
				Value:      val,
				Unit:       mf.unit,
				Timestamp:  now,
			})
		}
	}
}

// toFloat64 converts a JSON number (float64) or other numeric-like values.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
