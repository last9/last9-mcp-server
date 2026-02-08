package knowledge

import (
	"testing"
)

func TestMatchesPrefix(t *testing.T) {
	// nil / empty prefixes → always match
	if !matchesPrefix("anything", nil) {
		t.Error("nil prefixes should match")
	}
	if !matchesPrefix("anything", []string{}) {
		t.Error("empty prefixes should match")
	}

	// Single prefix match
	if !matchesPrefix("node_memory_total", []string{"node_"}) {
		t.Error("expected match for node_ prefix")
	}

	// No match
	if matchesPrefix("http_requests_total", []string{"node_"}) {
		t.Error("expected no match for http_requests_total with node_ prefix")
	}

	// Multiple prefixes, one matches
	if !matchesPrefix("node_cpu_seconds", []string{"kube_", "node_"}) {
		t.Error("expected match when one of multiple prefixes matches")
	}
}

func TestResolveEntities(t *testing.T) {
	// K8s labels → namespace, pod, container
	labels := map[string]interface{}{
		"__name__":  "kube_pod_container_resource_requests",
		"namespace": "default",
		"pod":       "nginx-abc",
		"container": "nginx",
		"node":      "worker-1",
	}
	entities := resolveEntities(labels, "kube_pod_container_resource_requests")

	if len(entities) != 4 {
		t.Fatalf("expected 4 entities, got %d", len(entities))
	}

	// Verify types present
	types := make(map[string]bool)
	for _, e := range entities {
		types[e.nodeType] = true
	}
	for _, want := range []string{"Namespace", "Pod", "Container", "VirtualMachine"} {
		if !types[want] {
			t.Errorf("missing entity type %s", want)
		}
	}

	// Verify scoped IDs
	for _, e := range entities {
		switch e.nodeType {
		case "Pod":
			if e.nodeID != MakeNodeID("Pod", "default", "nginx-abc") {
				t.Errorf("Pod nodeID = %s, want %s", e.nodeID, MakeNodeID("Pod", "default", "nginx-abc"))
			}
		case "Container":
			if e.nodeID != MakeNodeID("Container", "default", "nginx-abc", "nginx") {
				t.Errorf("Container nodeID = %s, want %s", e.nodeID, MakeNodeID("Container", "default", "nginx-abc", "nginx"))
			}
		case "Namespace":
			if e.nodeID != MakeNodeID("Namespace", "default") {
				t.Errorf("Namespace nodeID = %s, want %s", e.nodeID, MakeNodeID("Namespace", "default"))
			}
		}
	}

	// instance label only matches for node_ prefix metrics
	instanceLabels := map[string]interface{}{
		"__name__": "http_requests_total",
		"instance": "10.0.0.1:9090",
		"job":      "api",
	}
	entities = resolveEntities(instanceLabels, "http_requests_total")
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for non-node_ metric with instance label, got %d", len(entities))
	}

	// instance label with node_ prefix
	nodeLabels := map[string]interface{}{
		"__name__": "node_memory_total",
		"instance": "10.0.0.1:9100",
	}
	entities = resolveEntities(nodeLabels, "node_memory_total")
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity for node_ metric, got %d", len(entities))
	}
	if entities[0].nodeType != "VirtualMachine" {
		t.Errorf("expected VirtualMachine, got %s", entities[0].nodeType)
	}
}

func TestInferEdges(t *testing.T) {
	// Co-occurrence of namespace + pod + container → 2 edges
	entities := []resolvedEntity{
		{label: "namespace", nodeID: "namespace:default", nodeType: "Namespace"},
		{label: "pod", nodeID: "pod:default:nginx", nodeType: "Pod"},
		{label: "container", nodeID: "container:default:nginx:app", nodeType: "Container"},
	}

	edges := inferEdges(entities)
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(edges))
	}

	edgeMap := make(map[string]bool)
	for _, e := range edges {
		edgeMap[e.Relation] = true
	}
	if !edgeMap["CONTAINS"] {
		t.Error("missing CONTAINS edge")
	}
	if !edgeMap["RUNS"] {
		t.Error("missing RUNS edge")
	}

	// Single entity → no edges
	single := []resolvedEntity{
		{label: "namespace", nodeID: "namespace:default"},
	}
	edges = inferEdges(single)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for single entity, got %d", len(edges))
	}

	// Kafka co-occurrence
	kafka := []resolvedEntity{
		{label: "consumergroup", nodeID: "kafkaconsumergroup:cg1"},
		{label: "topic", nodeID: "kafkatopic:orders"},
	}
	edges = inferEdges(kafka)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge for kafka, got %d", len(edges))
	}
	if edges[0].Relation != "CONSUMES_FROM" {
		t.Errorf("expected CONSUMES_FROM, got %s", edges[0].Relation)
	}
}

func TestPickStatTarget(t *testing.T) {
	entities := []resolvedEntity{
		{nodeID: "namespace:default", priority: 1},
		{nodeID: "pod:default:nginx", priority: 3},
		{nodeID: "container:default:nginx:app", priority: 4},
	}

	target := pickStatTarget(entities)
	if target != "container:default:nginx:app" {
		t.Errorf("expected container as stat target, got %s", target)
	}

	// Empty → ""
	if pickStatTarget(nil) != "" {
		t.Error("expected empty string for nil entities")
	}
}

func TestResolveEntities_OTelK8sLabels(t *testing.T) {
	// OTel Collector kubelet receiver uses k8s_* prefixed label names
	labels := map[string]interface{}{
		"__name__":            "container_cpu_usage",
		"k8s_namespace_name":  "otel-demo",
		"k8s_deployment_name": "payment",
		"k8s_pod_name":        "payment-847455fc46-bnjhd",
		"k8s_container_name":  "payment",
		"k8s_node_name":       "ip-10-11-179-176.ap-south-1.compute.internal",
	}
	entities := resolveEntities(labels, "container_cpu_usage")

	if len(entities) != 5 {
		t.Fatalf("expected 5 entities (ns, deploy, pod, container, vm), got %d", len(entities))
	}

	types := make(map[string]bool)
	for _, e := range entities {
		types[e.nodeType] = true
	}
	for _, want := range []string{"Namespace", "Deployment", "Pod", "Container", "VirtualMachine"} {
		if !types[want] {
			t.Errorf("missing entity type %s", want)
		}
	}

	// Verify scoped IDs use k8s_* scope labels
	for _, e := range entities {
		switch e.nodeType {
		case "Pod":
			want := MakeNodeID("Pod", "otel-demo", "payment-847455fc46-bnjhd")
			if e.nodeID != want {
				t.Errorf("Pod nodeID = %s, want %s", e.nodeID, want)
			}
		case "Container":
			want := MakeNodeID("Container", "otel-demo", "payment-847455fc46-bnjhd", "payment")
			if e.nodeID != want {
				t.Errorf("Container nodeID = %s, want %s", e.nodeID, want)
			}
		case "Deployment":
			want := MakeNodeID("Deployment", "otel-demo", "payment")
			if e.nodeID != want {
				t.Errorf("Deployment nodeID = %s, want %s", e.nodeID, want)
			}
		}
	}
}

func TestInferEdges_OTelK8sLabels(t *testing.T) {
	// After alias resolution, resolvedEntity.label stores canonical names.
	// OTel k8s_* labels resolve to the same canonical names as KSM labels.
	entities := []resolvedEntity{
		{label: "namespace", nodeID: "namespace:otel-demo", nodeType: "Namespace"},
		{label: "deployment", nodeID: "deployment:otel-demo:payment", nodeType: "Deployment"},
		{label: "pod", nodeID: "pod:otel-demo:payment-abc", nodeType: "Pod"},
		{label: "container", nodeID: "container:otel-demo:payment-abc:payment", nodeType: "Container"},
		{label: "node", nodeID: "virtualmachine:worker-1", nodeType: "VirtualMachine"},
	}

	edges := inferEdges(entities)

	rels := make(map[string]bool)
	for _, e := range edges {
		rels[e.Relation] = true
	}
	// Expect: CONTAINS(ns→deploy), CONTAINS(ns→pod), MANAGES(deploy→pod), RUNS(pod→container), RUNS_ON(pod→vm)
	for _, want := range []string{"CONTAINS", "MANAGES", "RUNS", "RUNS_ON"} {
		if !rels[want] {
			t.Errorf("missing edge relation %s", want)
		}
	}
	if len(edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(edges))
		for _, e := range edges {
			t.Logf("  %s -[%s]-> %s", e.SourceID, e.Relation, e.TargetID)
		}
	}
}

func TestQualifyStatName(t *testing.T) {
	// With resource qualifier
	labels := map[string]interface{}{
		"resource": "cpu",
		"unit":     "core",
	}
	name, unit := qualifyStatName("kube_pod_container_resource_requests", labels)
	if name != "kube_pod_container_resource_requests:cpu" {
		t.Errorf("expected qualified name, got %s", name)
	}
	if unit != "core" {
		t.Errorf("expected unit=core, got %s", unit)
	}

	// No qualifier labels
	name, unit = qualifyStatName("http_requests_total", map[string]interface{}{})
	if name != "http_requests_total" {
		t.Errorf("expected unchanged name, got %s", name)
	}
	if unit != "" {
		t.Errorf("expected empty unit, got %s", unit)
	}
}

func TestResolveLabel(t *testing.T) {
	// KSM-style: canonical name matches directly
	val, ok := resolveLabel("namespace", map[string]interface{}{"namespace": "default"})
	if !ok || val != "default" {
		t.Errorf("expected (default, true), got (%s, %v)", val, ok)
	}

	// OTel-style: second alias matches
	val, ok = resolveLabel("namespace", map[string]interface{}{"k8s_namespace_name": "otel"})
	if !ok || val != "otel" {
		t.Errorf("expected (otel, true), got (%s, %v)", val, ok)
	}

	// No matching label present
	val, ok = resolveLabel("namespace", map[string]interface{}{"other": "x"})
	if ok || val != "" {
		t.Errorf("expected ('', false), got (%s, %v)", val, ok)
	}

	// service_name is an alias of "service" canonical
	val, ok = resolveLabel("service", map[string]interface{}{"service_name": "api"})
	if !ok || val != "api" {
		t.Errorf("expected (api, true), got (%s, %v)", val, ok)
	}

	// First alias wins when both are present
	val, ok = resolveLabel("service", map[string]interface{}{"service": "web", "service_name": "api"})
	if !ok || val != "web" {
		t.Errorf("expected (web, true) — first alias wins, got (%s, %v)", val, ok)
	}

	// Empty string values are treated as absent
	val, ok = resolveLabel("namespace", map[string]interface{}{"namespace": ""})
	if ok {
		t.Errorf("expected false for empty string value, got (%s, %v)", val, ok)
	}

	// Redpanda alias: "consumergroup" resolves from "redpanda_group"
	val, ok = resolveLabel("consumergroup", map[string]interface{}{"redpanda_group": "cg1"})
	if !ok || val != "cg1" {
		t.Errorf("expected (cg1, true), got (%s, %v)", val, ok)
	}

	// Unknown canonical with no aliases falls back to direct lookup
	val, ok = resolveLabel("unknown_label", map[string]interface{}{"unknown_label": "value"})
	if !ok || val != "value" {
		t.Errorf("expected (value, true) for unknown canonical fallback, got (%s, %v)", val, ok)
	}
}
