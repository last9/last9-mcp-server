package knowledge

import (
	"encoding/json"
	"os"
	"testing"
)

// --- DependencyGraphExtractor Tests ---

func TestDependencyGraphExtractor_CanHandle(t *testing.T) {
	e := &DependencyGraphExtractor{}

	validJSON := `{"service_name":"api-svc","incoming":{"web":{"Throughput":100}},"outgoing":{},"databases":{}}`
	var valid interface{}
	json.Unmarshal([]byte(validJSON), &valid)
	if !e.CanHandle(valid) {
		t.Error("expected CanHandle=true for valid dependency graph")
	}

	// Missing service_name
	invalidJSON := `{"incoming":{"web":{}}}`
	var invalid interface{}
	json.Unmarshal([]byte(invalidJSON), &invalid)
	if e.CanHandle(invalid) {
		t.Error("expected CanHandle=false without service_name")
	}

	// Array input
	arrayJSON := `[{"metric":{}}]`
	var arr interface{}
	json.Unmarshal([]byte(arrayJSON), &arr)
	if e.CanHandle(arr) {
		t.Error("expected CanHandle=false for array")
	}
}

func TestDependencyGraphExtractor_Extract(t *testing.T) {
	e := &DependencyGraphExtractor{}

	input := `{
		"service_name": "api-service",
		"env": "prod",
		"incoming": {
			"web-frontend": {"Throughput": 150.5, "ResponseTimeP95": 45.2, "ErrorRate": 2.3}
		},
		"outgoing": {
			"database-service": {"Throughput": 200.0}
		},
		"databases": {
			"postgres-primary": {"Throughput": 300.0, "ResponseTimeP95": 60.0}
		},
		"messaging_systems": {
			"kafka-cluster": {"Throughput": 500.0}
		}
	}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Expect: api-service, web-frontend, database-service, postgres-primary, kafka-cluster = 5 nodes
	if len(result.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(result.Nodes))
	}

	// All nodes should have env set to "prod"
	for _, n := range result.Nodes {
		if n.Env != "prod" {
			t.Errorf("expected env 'prod' on node %s, got %q", n.ID, n.Env)
		}
	}

	// Expect: web-frontend->api-service (CALLS), api-service->database-service (CALLS),
	//         api-service->postgres-primary (CONNECTS_TO), api-service->kafka-cluster (PRODUCES_TO) = 4 edges
	if len(result.Edges) != 4 {
		t.Errorf("expected 4 edges, got %d", len(result.Edges))
	}

	// Stats should be present for metrics
	if len(result.Stats) == 0 {
		t.Error("expected statistics to be extracted")
	}

	if result.Confidence < 0.8 {
		t.Errorf("expected high confidence, got %f", result.Confidence)
	}
}

func TestDependencyGraphExtractor_ExtraFieldsIgnored(t *testing.T) {
	e := &DependencyGraphExtractor{}

	input := `{
		"service_name": "api-service",
		"env": "prod",
		"unknown_field": "ignored",
		"incoming": {},
		"outgoing": {}
	}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(result.Nodes) != 1 { // just the root service
		t.Errorf("expected 1 node (root service only), got %d", len(result.Nodes))
	}
}

func TestDependencyGraphExtractor_EmptyServiceName(t *testing.T) {
	e := &DependencyGraphExtractor{}

	input := `{"service_name":"","incoming":{}}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Confidence > 0.2 {
		t.Errorf("expected low confidence for empty service_name, got %f", result.Confidence)
	}
}

// --- ComponentDiscoveryExtractor Tests ---

func TestComponentDiscoveryExtractor_CanHandle(t *testing.T) {
	e := &ComponentDiscoveryExtractor{}

	validJSON := `{"components":{"POD":["p1"],"SERVICE":["s1"]},"triples":[{"src":"s1","rel":"HAS","dst":"p1"}]}`
	var valid interface{}
	json.Unmarshal([]byte(validJSON), &valid)
	if !e.CanHandle(valid) {
		t.Error("expected CanHandle=true")
	}

	// Missing triples
	invalidJSON := `{"components":{"POD":["p1"]}}`
	var invalid interface{}
	json.Unmarshal([]byte(invalidJSON), &invalid)
	if e.CanHandle(invalid) {
		t.Error("expected CanHandle=false without triples")
	}

	// triples not an array
	badTriples := `{"components":{"POD":["p1"]},"triples":"not-array"}`
	var bad interface{}
	json.Unmarshal([]byte(badTriples), &bad)
	if e.CanHandle(bad) {
		t.Error("expected CanHandle=false when triples is not array")
	}
}

func TestComponentDiscoveryExtractor_Extract(t *testing.T) {
	e := &ComponentDiscoveryExtractor{}

	input := `{
		"components": {
			"POD": ["pod-1", "pod-2"],
			"SERVICE": ["service-1"],
			"CONTAINER": ["container-1"],
			"NAMESPACE": ["default"]
		},
		"triples": [
			{"src": "service-1", "rel": "HAS_ENDPOINTS", "dst": "pod-1"},
			{"src": "default", "rel": "CONTAINS", "dst": "service-1"}
		],
		"metrics": ["metric-1"]
	}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 5 nodes: pod-1, pod-2, service-1, container-1, default
	if len(result.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	// 2 edges from triples
	if len(result.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(result.Edges))
	}

	// Check type normalization
	for _, n := range result.Nodes {
		if n.Name == "pod-1" && n.Type != "Pod" {
			t.Errorf("expected type Pod for pod-1, got %s", n.Type)
		}
		if n.Name == "service-1" && n.Type != "Service" {
			t.Errorf("expected type Service for service-1, got %s", n.Type)
		}
	}
}

func TestComponentDiscoveryExtractor_EmptyComponents(t *testing.T) {
	e := &ComponentDiscoveryExtractor{}

	input := `{"components":{},"triples":[]}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Confidence > 0.3 {
		t.Errorf("expected low confidence for empty components, got %f", result.Confidence)
	}
}

// --- ServiceSummaryExtractor Tests ---

func TestServiceSummaryExtractor_CanHandle(t *testing.T) {
	e := &ServiceSummaryExtractor{}

	validJSON := `{"svc1":{"ServiceName":"svc1","Throughput":10},"svc2":{"ServiceName":"svc2","Throughput":20}}`
	var valid interface{}
	json.Unmarshal([]byte(validJSON), &valid)
	if !e.CanHandle(valid) {
		t.Error("expected CanHandle=true")
	}

	// Mixed: one value is not an object
	mixedJSON := `{"svc1":{"ServiceName":"svc1"},"bad":"string"}`
	var mixed interface{}
	json.Unmarshal([]byte(mixedJSON), &mixed)
	if e.CanHandle(mixed) {
		t.Error("expected CanHandle=false for mixed value types")
	}

	// Empty object
	emptyJSON := `{}`
	var empty interface{}
	json.Unmarshal([]byte(emptyJSON), &empty)
	if e.CanHandle(empty) {
		t.Error("expected CanHandle=false for empty object")
	}
}

func TestServiceSummaryExtractor_Extract(t *testing.T) {
	e := &ServiceSummaryExtractor{}

	input := `{
		"svc1": {"ServiceName": "svc1", "Env": "prod", "Throughput": 10.5, "ErrorRate": 0.5, "ResponseTime": 2.3},
		"svc2": {"ServiceName": "svc2", "Env": "staging", "Throughput": 20.0, "ErrorRate": 1.0, "ResponseTime": 5.1}
	}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(result.Nodes))
	}

	// Verify env propagation from inner objects
	envByName := map[string]string{}
	for _, n := range result.Nodes {
		envByName[n.Name] = n.Env
	}
	if envByName["svc1"] != "prod" {
		t.Errorf("expected env 'prod' for svc1, got %q", envByName["svc1"])
	}
	if envByName["svc2"] != "staging" {
		t.Errorf("expected env 'staging' for svc2, got %q", envByName["svc2"])
	}

	// Each service has 3 metrics = 6 stats total
	if len(result.Stats) != 6 {
		t.Errorf("expected 6 stats, got %d", len(result.Stats))
	}

	// No edges in service summary
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(result.Edges))
	}
}

// --- OperationsSummaryExtractor Tests ---

func TestOperationsSummaryExtractor_CanHandle(t *testing.T) {
	e := &OperationsSummaryExtractor{}

	validJSON := `{"service_name":"payment","operations":[{"name":"GET /api"}]}`
	var valid interface{}
	json.Unmarshal([]byte(validJSON), &valid)
	if !e.CanHandle(valid) {
		t.Error("expected CanHandle=true")
	}

	// Has service_name but no operations
	noOps := `{"service_name":"payment","incoming":{}}`
	var noOpsP interface{}
	json.Unmarshal([]byte(noOps), &noOpsP)
	if e.CanHandle(noOpsP) {
		t.Error("expected CanHandle=false without operations array")
	}

	// operations is not an array
	badOps := `{"service_name":"payment","operations":"not-array"}`
	var badOpsP interface{}
	json.Unmarshal([]byte(badOps), &badOpsP)
	if e.CanHandle(badOpsP) {
		t.Error("expected CanHandle=false when operations is not array")
	}
}

func TestOperationsSummaryExtractor_Extract(t *testing.T) {
	e := &OperationsSummaryExtractor{}

	input := `{
		"service_name": "payment-service",
		"env": "prod",
		"operations": [
			{
				"name": "ProcessPayment",
				"db_system": "postgresql",
				"net_peer_name": "db.internal",
				"throughput": 150.5,
				"error_rate": 2.3,
				"response_time": {"p50": 25.5, "p95": 45.2}
			},
			{
				"name": "SendNotification",
				"messaging_system": "kafka",
				"throughput": 300.0
			},
			{
				"name": "HealthCheck"
			}
		]
	}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Nodes: payment-service, ProcessPayment endpoint, postgresql DataStore,
	//        SendNotification endpoint, kafka topic, HealthCheck endpoint = 6
	if len(result.Nodes) != 6 {
		t.Errorf("expected 6 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s) %s", n.ID, n.Type, n.Name)
		}
	}

	// All nodes should have env set to "prod"
	for _, n := range result.Nodes {
		if n.Env != "prod" {
			t.Errorf("expected env 'prod' on node %s, got %q", n.ID, n.Env)
		}
	}

	// Edges: 3 EXPOSES (svc->endpoint), 1 CONNECTS_TO (endpoint->db), 1 PRODUCES_TO (endpoint->kafka) = 5
	if len(result.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(result.Edges))
		for _, e := range result.Edges {
			t.Logf("  edge: %s -[%s]-> %s", e.SourceID, e.Relation, e.TargetID)
		}
	}

	if len(result.Stats) == 0 {
		t.Error("expected statistics to be extracted")
	}
}

func TestOperationsSummaryExtractor_EmptyOperations(t *testing.T) {
	e := &OperationsSummaryExtractor{}

	input := `{"service_name":"svc","operations":[]}`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	// Only the root service node
	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(result.Nodes))
	}
	if result.Confidence > 0.5 {
		t.Errorf("expected low confidence for empty operations, got %f", result.Confidence)
	}
}

// --- PrometheusExtractor Tests ---

func TestPrometheusExtractor_CanHandle(t *testing.T) {
	e := &PrometheusExtractor{}

	// Instant query shape
	instantJSON := `[{"metric":{"__name__":"up","service_name":"api"},"value":[1700000000,"1"]}]`
	var instant interface{}
	json.Unmarshal([]byte(instantJSON), &instant)
	if !e.CanHandle(instant) {
		t.Error("expected CanHandle=true for instant query")
	}

	// Range query shape
	rangeJSON := `[{"metric":{"__name__":"up"},"values":[[1700000000,"1"],[1700000060,"1"]]}]`
	var rangeP interface{}
	json.Unmarshal([]byte(rangeJSON), &rangeP)
	if !e.CanHandle(rangeP) {
		t.Error("expected CanHandle=true for range query")
	}

	// Empty array
	emptyJSON := `[]`
	var empty interface{}
	json.Unmarshal([]byte(emptyJSON), &empty)
	if e.CanHandle(empty) {
		t.Error("expected CanHandle=false for empty array")
	}

	// Object (not array)
	objJSON := `{"service_name":"x"}`
	var obj interface{}
	json.Unmarshal([]byte(objJSON), &obj)
	if e.CanHandle(obj) {
		t.Error("expected CanHandle=false for non-array")
	}
}

func TestPrometheusExtractor_ExtractInstant(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[
		{"metric":{"__name__":"http_requests_total","service_name":"api","method":"GET"},"value":[1700000000,"42.5"]},
		{"metric":{"__name__":"http_requests_total","service_name":"web","method":"POST"},"value":[1700000000,"100"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 2 stats (one per series)
	if len(result.Stats) != 2 {
		t.Errorf("expected 2 stats, got %d", len(result.Stats))
	}

	// service_name label now creates Service nodes
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes (service:api, service:web), got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	// Check value parsing from string
	for _, s := range result.Stats {
		if s.MetricName == "http_requests_total" && s.NodeID == "service:api" {
			if s.Value != 42.5 {
				t.Errorf("expected value 42.5, got %f", s.Value)
			}
		}
	}
}

func TestPrometheusExtractor_ExtractRange(t *testing.T) {
	e := &PrometheusExtractor{}

	// Range query: most recent value should be used
	input := `[
		{"metric":{"__name__":"cpu_usage","job":"node-exporter"},"values":[[1700000000,"0.5"],[1700000060,"0.6"],[1700000120,"0.7"]]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(result.Stats))
	}

	// Most recent value should be 0.7
	if result.Stats[0].Value != 0.7 {
		t.Errorf("expected most recent value 0.7, got %f", result.Stats[0].Value)
	}

	// Node ID should be from "job" label (no service_name)
	if result.Stats[0].NodeID != "service:node-exporter" {
		t.Errorf("expected nodeID service:node-exporter, got %s", result.Stats[0].NodeID)
	}
}

func TestPrometheusExtractor_EmptyValues(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[{"metric":{"__name__":"up"},"values":[]}]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Confidence > 0.3 {
		t.Errorf("expected low confidence for empty values, got %f", result.Confidence)
	}
}

// --- ExtractorRegistry Tests ---

func TestExtractorRegistry_DispatchOrder(t *testing.T) {
	registry := NewExtractorRegistry()

	// ComponentDiscovery should match before others
	compJSON := `{"components":{"POD":["p1"]},"triples":[]}`
	var comp interface{}
	json.Unmarshal([]byte(compJSON), &comp)

	result, err := registry.TryExtract(comp)
	if err != nil {
		t.Fatalf("TryExtract failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Pattern != "component_discovery" {
		t.Errorf("expected component_discovery, got %s", result.Pattern)
	}

	// Dependency graph
	depJSON := `{"service_name":"svc","incoming":{}}`
	var dep interface{}
	json.Unmarshal([]byte(depJSON), &dep)

	result, err = registry.TryExtract(dep)
	if err != nil {
		t.Fatalf("TryExtract failed: %v", err)
	}
	if result.Pattern != "dependency_graph" {
		t.Errorf("expected dependency_graph, got %s", result.Pattern)
	}

	// Unrecognized shape returns nil
	unknownJSON := `{"random":"data"}`
	var unknown interface{}
	json.Unmarshal([]byte(unknownJSON), &unknown)

	result, err = registry.TryExtract(unknown)
	if err != nil {
		t.Fatalf("TryExtract failed: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for unrecognized shape, got %s", result.Pattern)
	}
}

// --- Pipeline Integration Tests ---

func TestPipeline_JSONToExtraction(t *testing.T) {
	pipeline := NewPipeline()

	input := `{"service_name":"api","incoming":{"web":{"Throughput":100}},"outgoing":{}}`
	result, err := pipeline.Process(input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes, got %d", len(result.Nodes))
	}
	if result.Pattern != "dependency_graph" {
		t.Errorf("expected pattern dependency_graph, got %s", result.Pattern)
	}
}

func TestPipeline_PlainTextDrainWithRule(t *testing.T) {
	pipeline := NewPipeline()

	// Pre-seed Drain with a generalized template that triggers the hardcoded
	// "Connection to <*> failed" mapping rule. The simplified Drain impl makes
	// it hard to naturally reach template generalization via parsing alone, so
	// we set the internal state directly.
	pipeline.drain.Clusters["C1"] = &LogCluster{
		ID:       "C1",
		Template: []string{"Connection", "to", "<*>", "failed"},
		RawLogs:  make(CountMap),
	}

	// Build tree: length "4" -> "Connection" -> "to" -> "db:mysql" -> leaf(C1)
	// The Drain traversal does exact match at each level, so when we parse
	// "Connection to db:mysql failed", each token needs an exact-match child.
	lengthNode := &DrainNode{Children: make(map[string]*DrainNode)}
	connNode := &DrainNode{Children: make(map[string]*DrainNode)}
	toNode := &DrainNode{Children: make(map[string]*DrainNode)}
	dbNode := &DrainNode{Children: make(map[string]*DrainNode)}
	leafNode := &DrainNode{IsLeaf: true, ClusterID: "C1", Children: make(map[string]*DrainNode)}

	pipeline.drain.Root.Children["4"] = lengthNode
	lengthNode.Children["Connection"] = connNode
	connNode.Children["to"] = toNode
	toNode.Children["db:mysql"] = dbNode
	dbNode.Children["failed"] = leafNode

	result, err := pipeline.Process("Connection to db:mysql failed")
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Pattern != "drain" {
		t.Errorf("expected drain pattern, got %s", result.Pattern)
	}
	if len(result.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes, got %d", len(result.Nodes))
	}

	// Verify the target node
	found := false
	for _, n := range result.Nodes {
		if n.ID == "db:mysql" {
			found = true
		}
	}
	if !found {
		t.Error("expected node with ID db:mysql")
	}
}

func TestPipeline_PlainTextUnmappedTemplate(t *testing.T) {
	pipeline := NewPipeline()

	// First parse creates a cluster with exact template. Since no mapping rule
	// exists for this template, the pipeline returns an error.
	_, err := pipeline.Process("User logged in from 192.168.1.1")
	if err == nil {
		t.Error("expected error for unmapped plain text template")
	}
}

func TestPipeline_UnrecognizedJSON(t *testing.T) {
	pipeline := NewPipeline()

	input := `{"random":"data","with":"no","known":"pattern"}`
	_, err := pipeline.Process(input)
	if err == nil {
		t.Error("expected error for unrecognized JSON")
	}
}

func TestPipeline_EndToEnd_IngestAndSearch(t *testing.T) {
	tmpDB := "test_pipeline_e2e.db"
	store, err := NewStore(tmpDB)
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}
	defer func() {
		store.Close()
		os.Remove(tmpDB)
		os.Remove(tmpDB + "-shm")
		os.Remove(tmpDB + "-wal")
	}()

	pipeline := NewPipeline()

	// Process structured JSON
	input := `{
		"service_name": "payment-api",
		"incoming": {"checkout": {"Throughput": 100}},
		"outgoing": {},
		"databases": {"postgres": {"Throughput": 200}}
	}`
	result, err := pipeline.Process(input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Ingest the extracted data
	ctx := t
	_ = ctx
	if err := store.IngestNodes(t.Context(), result.Nodes); err != nil {
		t.Fatalf("IngestNodes failed: %v", err)
	}
	if err := store.IngestEdges(t.Context(), result.Edges); err != nil {
		t.Fatalf("IngestEdges failed: %v", err)
	}
	if err := store.IngestStatistics(t.Context(), result.Stats); err != nil {
		t.Fatalf("IngestStatistics failed: %v", err)
	}

	// Search for the service
	searchResult, err := store.Search(t.Context(), "payment", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(searchResult.Nodes) == 0 {
		t.Error("expected search to find payment-api node")
	}
}

// --- Prometheus Topology Extraction Tests ---

func TestPrometheusExtract_KSMTopology(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[
		{"metric":{"__name__":"kube_pod_container_resource_requests","namespace":"default","pod":"nginx-abc","container":"nginx","node":"worker-1","resource":"cpu","unit":"core"},"value":[1700000000,"0.5"]},
		{"metric":{"__name__":"kube_pod_container_resource_requests","namespace":"default","pod":"nginx-abc","container":"nginx","node":"worker-1","resource":"memory","unit":"byte"},"value":[1700000000,"134217728"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Nodes: Namespace(default), Pod(nginx-abc), Container(nginx), VirtualMachine(worker-1)
	if len(result.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	// Verify node types
	typeCount := make(map[string]int)
	for _, n := range result.Nodes {
		typeCount[n.Type]++
	}
	for _, want := range []string{"Namespace", "Pod", "Container", "VirtualMachine"} {
		if typeCount[want] != 1 {
			t.Errorf("expected 1 %s node, got %d", want, typeCount[want])
		}
	}

	// Edges: CONTAINS(namespace→pod), RUNS(pod→container), RUNS_ON(pod→node)
	if len(result.Edges) != 3 {
		t.Errorf("expected 3 edges, got %d", len(result.Edges))
		for _, e := range result.Edges {
			t.Logf("  edge: %s -[%s]-> %s", e.SourceID, e.Relation, e.TargetID)
		}
	}

	edgeRels := make(map[string]bool)
	for _, e := range result.Edges {
		edgeRels[e.Relation] = true
	}
	for _, want := range []string{"CONTAINS", "RUNS", "RUNS_ON"} {
		if !edgeRels[want] {
			t.Errorf("missing edge relation %s", want)
		}
	}

	// Stats: attached to Container (highest priority), qualified with resource
	if len(result.Stats) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(result.Stats))
	}
	for _, s := range result.Stats {
		wantNodeID := MakeNodeID("Container", "default", "nginx-abc", "nginx")
		if s.NodeID != wantNodeID {
			t.Errorf("stat nodeID = %s, want %s", s.NodeID, wantNodeID)
		}
		if s.Unit == "" {
			t.Error("expected unit to be set")
		}
	}

	// Check metric name qualification
	nameSet := make(map[string]bool)
	for _, s := range result.Stats {
		nameSet[s.MetricName] = true
	}
	if !nameSet["kube_pod_container_resource_requests:cpu"] {
		t.Error("missing qualified metric name kube_pod_container_resource_requests:cpu")
	}
	if !nameSet["kube_pod_container_resource_requests:memory"] {
		t.Error("missing qualified metric name kube_pod_container_resource_requests:memory")
	}
}

func TestPrometheusExtract_NodeExporter(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[
		{"metric":{"__name__":"node_memory_MemTotal_bytes","instance":"10.0.0.1:9100","job":"node-exporter"},"value":[1700000000,"8388608000"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 1 VirtualMachine node from instance label (node_ prefix match)
	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}
	if len(result.Nodes) > 0 && result.Nodes[0].Type != "VirtualMachine" {
		t.Errorf("expected VirtualMachine, got %s", result.Nodes[0].Type)
	}

	// No edges (single entity)
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(result.Edges))
	}

	// Stat attached to VM node
	if len(result.Stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(result.Stats))
	}
	if result.Stats[0].NodeID != result.Nodes[0].ID {
		t.Errorf("stat nodeID = %s, want %s", result.Stats[0].NodeID, result.Nodes[0].ID)
	}
}

func TestPrometheusExtract_NodeExporterInstanceIgnoredForNonNodeMetrics(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[
		{"metric":{"__name__":"http_requests_total","instance":"10.0.0.1:9090","job":"api"},"value":[1700000000,"100"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// No nodes — instance rule requires node_ prefix
	if len(result.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	// No edges
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(result.Edges))
	}

	// Stat falls back to legacy nodeID
	if len(result.Stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(result.Stats))
	}
	// Legacy resolvePrometheusNodeID: job=api → service:api
	if result.Stats[0].NodeID != "service:api" {
		t.Errorf("expected fallback nodeID service:api, got %s", result.Stats[0].NodeID)
	}
}

func TestPrometheusExtract_Kafka(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[
		{"metric":{"__name__":"kafka_consumergroup_lag","consumergroup":"order-processor","topic":"orders"},"value":[1700000000,"150"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 2 nodes: KafkaConsumerGroup, KafkaTopic
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	typeSet := make(map[string]bool)
	for _, n := range result.Nodes {
		typeSet[n.Type] = true
	}
	if !typeSet["KafkaConsumerGroup"] {
		t.Error("missing KafkaConsumerGroup node")
	}
	if !typeSet["KafkaTopic"] {
		t.Error("missing KafkaTopic node")
	}

	// 1 edge: CONSUMES_FROM
	if len(result.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(result.Edges))
	}
	if len(result.Edges) > 0 && result.Edges[0].Relation != "CONSUMES_FROM" {
		t.Errorf("expected CONSUMES_FROM, got %s", result.Edges[0].Relation)
	}

	// Stat attached (both have priority 2, first highest wins)
	if len(result.Stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(result.Stats))
	}
}

func TestPrometheusExtract_Deduplication(t *testing.T) {
	e := &PrometheusExtractor{}

	// 3 series sharing same pod/namespace but different containers
	input := `[
		{"metric":{"__name__":"container_cpu","namespace":"default","pod":"nginx-abc","container":"nginx"},"value":[1700000000,"0.1"]},
		{"metric":{"__name__":"container_cpu","namespace":"default","pod":"nginx-abc","container":"sidecar"},"value":[1700000000,"0.05"]},
		{"metric":{"__name__":"container_cpu","namespace":"default","pod":"nginx-abc","container":"init"},"value":[1700000000,"0"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 1 Namespace + 1 Pod + 3 Containers = 5 nodes
	if len(result.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	typeCount := make(map[string]int)
	for _, n := range result.Nodes {
		typeCount[n.Type]++
	}
	if typeCount["Namespace"] != 1 {
		t.Errorf("expected 1 Namespace, got %d", typeCount["Namespace"])
	}
	if typeCount["Pod"] != 1 {
		t.Errorf("expected 1 Pod, got %d", typeCount["Pod"])
	}
	if typeCount["Container"] != 3 {
		t.Errorf("expected 3 Containers, got %d", typeCount["Container"])
	}

	// 3 stats
	if len(result.Stats) != 3 {
		t.Errorf("expected 3 stats, got %d", len(result.Stats))
	}
}

func TestPrometheusExtract_Fallback(t *testing.T) {
	e := &PrometheusExtractor{}

	// Only job + instance labels, non-matching prefix for instance rule
	input := `[
		{"metric":{"__name__":"custom_metric","job":"myapp","instance":"10.0.0.1:8080"},"value":[1700000000,"42"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 0 nodes, 0 edges
	if len(result.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(result.Nodes))
	}
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(result.Edges))
	}

	// Stat with legacy nodeID from resolvePrometheusNodeID (job=myapp → service:myapp)
	if len(result.Stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(result.Stats))
	}
	if result.Stats[0].NodeID != MakeNodeID("Service", "myapp") {
		t.Errorf("expected fallback nodeID %s, got %s", MakeNodeID("Service", "myapp"), result.Stats[0].NodeID)
	}
}

func TestPrometheusExtract_EnvFromClusterLabel(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[
		{"metric":{"__name__":"kube_pod_container_resource_requests","namespace":"default","pod":"nginx-abc","container":"nginx","node":"worker-1","cluster":"minikube","resource":"cpu"},"value":[1700000000,"0.5"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, n := range result.Nodes {
		if n.Env != "minikube" {
			t.Errorf("node %s (%s): expected env 'minikube', got %q", n.ID, n.Type, n.Env)
		}
	}
}

func TestPrometheusExtract_EnvPriority(t *testing.T) {
	e := &PrometheusExtractor{}

	// environment label should beat cluster
	input := `[
		{"metric":{"__name__":"kube_pod_info","namespace":"default","pod":"nginx-abc","environment":"production","cluster":"minikube"},"value":[1700000000,"1"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, n := range result.Nodes {
		if n.Env != "production" {
			t.Errorf("node %s (%s): expected env 'production', got %q", n.ID, n.Type, n.Env)
		}
	}
}

func TestPrometheusExtract_EnvDedupUpgrade(t *testing.T) {
	e := &PrometheusExtractor{}

	// First series: same pod but no env label. Second series: same pod with cluster label.
	// The node should get upgraded to have env set.
	input := `[
		{"metric":{"__name__":"container_cpu","namespace":"default","pod":"nginx-abc","container":"nginx"},"value":[1700000000,"0.1"]},
		{"metric":{"__name__":"kube_pod_info","namespace":"default","pod":"nginx-abc","cluster":"minikube"},"value":[1700000000,"1"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// The shared nodes (Namespace:default, Pod:default:nginx-abc) should have env upgraded
	for _, n := range result.Nodes {
		if n.Type == "Namespace" || n.Type == "Pod" {
			if n.Env != "minikube" {
				t.Errorf("node %s (%s): expected env 'minikube' after dedup upgrade, got %q", n.ID, n.Type, n.Env)
			}
		}
	}
}

func TestPrometheusExtract_NoEnvLabel(t *testing.T) {
	e := &PrometheusExtractor{}

	input := `[
		{"metric":{"__name__":"kube_pod_info","namespace":"default","pod":"nginx-abc"},"value":[1700000000,"1"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, n := range result.Nodes {
		if n.Env != "" {
			t.Errorf("node %s (%s): expected empty env, got %q", n.ID, n.Type, n.Env)
		}
	}
}

func TestPrometheusExtract_OTelK8sLabels(t *testing.T) {
	e := &PrometheusExtractor{}

	// OTel Collector kubelet receiver uses k8s_* prefixed labels
	input := `[
		{"metric":{"__name__":"container_cpu_usage","k8s_namespace_name":"otel-demo","k8s_deployment_name":"payment","k8s_pod_name":"payment-847455fc46-bnjhd","k8s_container_name":"payment","k8s_node_name":"ip-10-11-179-176"},"value":[1700000000,"0.003"]},
		{"metric":{"__name__":"container_memory_working_set_bytes","k8s_namespace_name":"otel-demo","k8s_deployment_name":"payment","k8s_pod_name":"payment-847455fc46-bnjhd","k8s_container_name":"payment","k8s_node_name":"ip-10-11-179-176"},"value":[1700000000,"117000000"]}
	]`
	var parsed interface{}
	json.Unmarshal([]byte(input), &parsed)

	result, err := e.Extract(parsed)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Nodes: Namespace, Deployment, Pod, Container, VirtualMachine = 5
	if len(result.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	typeCount := make(map[string]int)
	for _, n := range result.Nodes {
		typeCount[n.Type]++
	}
	for _, want := range []string{"Namespace", "Deployment", "Pod", "Container", "VirtualMachine"} {
		if typeCount[want] != 1 {
			t.Errorf("expected 1 %s node, got %d", want, typeCount[want])
		}
	}

	// Edges: CONTAINS(ns→deploy), CONTAINS(ns→pod), MANAGES(deploy→pod),
	//        RUNS(pod→container), RUNS_ON(pod→vm) = 5
	if len(result.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(result.Edges))
		for _, e := range result.Edges {
			t.Logf("  edge: %s -[%s]-> %s", e.SourceID, e.Relation, e.TargetID)
		}
	}

	// 2 stats, both on Container (highest priority)
	if len(result.Stats) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(result.Stats))
	}
	wantContainer := MakeNodeID("Container", "otel-demo", "payment-847455fc46-bnjhd", "payment")
	for _, s := range result.Stats {
		if s.NodeID != wantContainer {
			t.Errorf("stat nodeID = %s, want %s", s.NodeID, wantContainer)
		}
	}
}

// --- MatchSchemasScored Tests ---

func TestMatchSchemasScored(t *testing.T) {
	schemas := []Schema{
		{
			Name: "http_k8s_datastore",
			Blueprint: SchemaBlueprint{
				Nodes: []string{"Service", "HTTPEndpoint", "DataStoreInstance", "Pod", "Container"},
				Edges: []string{
					"Service -> EXPOSES -> HTTPEndpoint",
					"Service -> CALLS -> Service",
					"Container -> CONNECTS_TO -> DataStoreInstance",
				},
			},
		},
		{
			Name: "kafka_consumer_jobs",
			Blueprint: SchemaBlueprint{
				Nodes: []string{"ConsumerJob", "KafkaTopic", "DataStoreInstance"},
				Edges: []string{
					"ConsumerJob -> CONSUMES_FROM -> KafkaTopic",
					"ConsumerJob -> WRITES_TO -> DataStoreInstance",
				},
			},
		},
	}

	// Input that matches http_k8s_datastore
	nodes := []Node{
		{ID: "service:api", Type: "Service"},
		{ID: "service:web", Type: "Service"},
		{ID: "datastoreinstance:postgres", Type: "DataStoreInstance"},
	}
	edges := []Edge{
		{SourceID: "service:api", TargetID: "service:web", Relation: "CALLS"},
	}

	matches := MatchSchemasScored(nodes, edges, schemas)
	if len(matches) == 0 {
		t.Fatal("expected at least one schema match")
	}
	if matches[0].Name != "http_k8s_datastore" {
		t.Errorf("expected http_k8s_datastore as top match, got %s", matches[0].Name)
	}
}

func TestMatchSchemasScored_NoEdges(t *testing.T) {
	schemas := []Schema{
		{
			Name: "test",
			Blueprint: SchemaBlueprint{
				Nodes: []string{"Service"},
				Edges: []string{"Service -> CALLS -> Service"},
			},
		},
	}

	// Nodes only, no edges — should still score on node coverage + field confidence
	nodes := []Node{
		{ID: "service:api", Type: "Service"},
	}

	matches := MatchSchemasScored(nodes, nil, schemas)
	// With only node coverage (0.3*1.0) + field confidence (0.2*1.0) = 0.5 < 0.6, no match
	if len(matches) != 0 {
		t.Errorf("expected no matches for nodes-only input below threshold, got %d", len(matches))
	}
}
