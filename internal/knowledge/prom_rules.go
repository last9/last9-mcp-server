package knowledge

import "strings"

// entityRule maps a canonical Prometheus label name to a knowledge-graph node
// type. MetricPrefixes constrains the rule to metrics whose __name__ starts
// with one of the listed prefixes (empty means "all metrics"). ScopeLabels
// lists extra canonical labels whose values are included in MakeNodeID for
// uniqueness. Priority controls stat attachment: higher = more specific entity.
type entityRule struct {
	Label          string   // canonical label name (resolved via labelAliases)
	NodeType       string
	MetricPrefixes []string // empty = match all metrics
	ScopeLabels    []string // canonical label names
	Priority       int      // higher = more specific
}

// edgeRule maps the co-occurrence of two entity-identifying labels on the same
// Prometheus series to a directed relationship. Labels are canonical names.
type edgeRule struct {
	SourceLabel string
	TargetLabel string
	Relation    string
}

// labelAliases maps a canonical label name to all known Prometheus label name
// variants. When resolving a canonical label against a series, each alias is
// tried in order and the first match wins. This eliminates duplicate entity and
// edge rules for different label naming conventions (KSM vs OTel, Kafka vs
// Redpanda).
var labelAliases = map[string][]string{
	"namespace":     {"namespace", "k8s_namespace_name"},
	"deployment":    {"deployment", "k8s_deployment_name"},
	"service":       {"service", "service_name"},
	"pod":           {"pod", "k8s_pod_name"},
	"container":     {"container", "k8s_container_name"},
	"node":          {"node", "k8s_node_name"},
	"instance":      {"instance"},
	"topic":         {"topic", "redpanda_topic"},
	"consumergroup": {"consumergroup", "redpanda_group"},
}

// resolveLabel finds the value of a canonical label in a series by trying
// each alias in order. Returns the value and true if found.
func resolveLabel(canonical string, labels map[string]interface{}) (string, bool) {
	aliases := labelAliases[canonical]
	if len(aliases) == 0 {
		// No alias defined; try canonical name directly as fallback.
		val, ok := labels[canonical].(string)
		return val, ok && val != ""
	}
	for _, alias := range aliases {
		if val, ok := labels[alias].(string); ok && val != "" {
			return val, true
		}
	}
	return "", false
}

// defaultEntityRules encode what Prometheus labels semantically mean,
// independent of any registered schema. Rules use canonical label names;
// actual Prometheus labels are resolved via labelAliases. Only `instance` has
// a metric prefix constraint because it appears on ALL Prometheus metrics.
var defaultEntityRules = []entityRule{
	{Label: "namespace", NodeType: "Namespace", Priority: 1},
	{Label: "deployment", NodeType: "Deployment", ScopeLabels: []string{"namespace"}, Priority: 2},
	{Label: "service", NodeType: "Service", ScopeLabels: []string{"namespace"}, Priority: 2},
	{Label: "pod", NodeType: "Pod", ScopeLabels: []string{"namespace"}, Priority: 3},
	{Label: "container", NodeType: "Container", ScopeLabels: []string{"namespace", "pod"}, Priority: 4},
	{Label: "node", NodeType: "VirtualMachine", Priority: 1},
	{Label: "instance", NodeType: "VirtualMachine", MetricPrefixes: []string{"node_"}, Priority: 1},
	{Label: "topic", NodeType: "KafkaTopic", Priority: 2},
	{Label: "consumergroup", NodeType: "KafkaConsumerGroup", Priority: 2},
}

// defaultEdgeRules encode relationships inferable from label co-occurrence.
// Labels are canonical names resolved via labelAliases.
var defaultEdgeRules = []edgeRule{
	{SourceLabel: "namespace", TargetLabel: "deployment", Relation: "CONTAINS"},
	{SourceLabel: "namespace", TargetLabel: "service", Relation: "CONTAINS"},
	{SourceLabel: "namespace", TargetLabel: "pod", Relation: "CONTAINS"},
	{SourceLabel: "deployment", TargetLabel: "pod", Relation: "MANAGES"},
	{SourceLabel: "pod", TargetLabel: "container", Relation: "RUNS"},
	{SourceLabel: "pod", TargetLabel: "node", Relation: "RUNS_ON"},
	{SourceLabel: "consumergroup", TargetLabel: "topic", Relation: "CONSUMES_FROM"},
}

// statQualifierLabels are label names whose values are appended to the metric
// name to disambiguate (e.g. resource=cpu → "metric_name:cpu").
var statQualifierLabels = []string{"resource"}

// resolvedEntity records a single entity found in a Prometheus series.
type resolvedEntity struct {
	label    string // Prometheus label that matched
	nodeID   string
	nodeType string
	name     string // raw label value
	priority int
}

// matchesPrefix returns true if prefixes is empty or metricName starts with
// any of the listed prefixes.
func matchesPrefix(metricName string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(metricName, p) {
			return true
		}
	}
	return false
}

// resolveEntities iterates defaultEntityRules and returns all entities whose
// canonical label resolves to a value in the series and whose prefix constraint
// is satisfied. Canonical label names are resolved via labelAliases.
func resolveEntities(labels map[string]interface{}, metricName string) []resolvedEntity {
	var out []resolvedEntity
	for _, rule := range defaultEntityRules {
		val, ok := resolveLabel(rule.Label, labels)
		if !ok {
			continue
		}
		if !matchesPrefix(metricName, rule.MetricPrefixes) {
			continue
		}
		// Build scoped ID parts: scope label values followed by the label value.
		parts := make([]string, 0, len(rule.ScopeLabels)+1)
		for _, sl := range rule.ScopeLabels {
			if sv, ok := resolveLabel(sl, labels); ok {
				parts = append(parts, sv)
			}
		}
		parts = append(parts, val)

		out = append(out, resolvedEntity{
			label:    rule.Label, // stores canonical name
			nodeID:   MakeNodeID(rule.NodeType, parts...),
			nodeType: rule.NodeType,
			name:     val,
			priority: rule.Priority,
		})
	}
	return out
}

// inferEdges checks defaultEdgeRules against the resolved entities. If both
// the source and target labels were resolved, an edge is emitted.
func inferEdges(resolved []resolvedEntity) []Edge {
	// Build label → nodeID lookup.
	byLabel := make(map[string]string, len(resolved))
	for _, e := range resolved {
		byLabel[e.label] = e.nodeID
	}

	var out []Edge
	for _, rule := range defaultEdgeRules {
		srcID, hasSrc := byLabel[rule.SourceLabel]
		tgtID, hasTgt := byLabel[rule.TargetLabel]
		if hasSrc && hasTgt {
			out = append(out, Edge{
				SourceID: srcID,
				TargetID: tgtID,
				Relation: rule.Relation,
			})
		}
	}
	return out
}

// pickStatTarget returns the nodeID of the highest-priority resolved entity.
// On a tie, the first one encountered wins (rule table order). Returns "" if
// the slice is empty.
func pickStatTarget(resolved []resolvedEntity) string {
	best := ""
	bestPri := -1
	for _, e := range resolved {
		if e.priority > bestPri {
			bestPri = e.priority
			best = e.nodeID
		}
	}
	return best
}

// qualifyStatName appends qualifier label values to the metric name and
// extracts the unit label if present.
func qualifyStatName(metricName string, labels map[string]interface{}) (name string, unit string) {
	name = metricName
	for _, ql := range statQualifierLabels {
		if v, ok := labels[ql].(string); ok && v != "" {
			name = name + ":" + v
		}
	}
	if u, ok := labels["unit"].(string); ok && u != "" {
		unit = u
	}
	return name, unit
}
