package knowledge

import (
	"strings"
)

// ComponentDiscoveryExtractor handles output of discover_system_components.
//
// Expected shape:
//
//	{
//	  "components": {"POD": [...], "SERVICE": [...]},
//	  "triples": [{"src": "...", "rel": "...", "dst": "..."}]
//	}
type ComponentDiscoveryExtractor struct{}

func (e *ComponentDiscoveryExtractor) Name() string { return "component_discovery" }

// CanHandle checks for the distinctive shape: top-level object with "components" (map)
// AND "triples" (array).
func (e *ComponentDiscoveryExtractor) CanHandle(parsed interface{}) bool {
	obj, ok := parsed.(map[string]interface{})
	if !ok {
		return false
	}

	components, hasComponents := obj["components"]
	triples, hasTriples := obj["triples"]
	if !hasComponents || !hasTriples {
		return false
	}

	// components must be a map, triples must be an array
	_, compIsMap := components.(map[string]interface{})
	_, tripIsArr := triples.([]interface{})
	return compIsMap && tripIsArr
}

// typeNormalization maps uppercase component type keys from the discovery API
// to schema-compatible PascalCase node types.
var typeNormalization = map[string]string{
	"POD":        "Pod",
	"SERVICE":    "Service",
	"CONTAINER":  "Container",
	"NAMESPACE":  "Namespace",
	"DEPLOYMENT": "Deployment",
	"NODE":       "Node",
}

// normalizeType converts an uppercase type key to its PascalCase form.
// Falls back to title-casing the input if no mapping exists.
func normalizeType(raw string) string {
	if mapped, ok := typeNormalization[raw]; ok {
		return mapped
	}
	// Fallback: lowercase then capitalize first letter
	if raw == "" {
		return "Unknown"
	}
	lower := strings.ToLower(raw)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func (e *ComponentDiscoveryExtractor) Extract(parsed interface{}) (*ExtractionResult, error) {
	obj := parsed.(map[string]interface{})

	result := &ExtractionResult{
		Confidence: 0.9,
	}

	// Track created node IDs to avoid duplicates
	nodeSet := make(map[string]bool)

	// Extract nodes from components map
	components, _ := obj["components"].(map[string]interface{})
	for typeKey, listRaw := range components {
		nodeType := normalizeType(typeKey)
		list, ok := listRaw.([]interface{})
		if !ok {
			continue
		}
		for _, nameRaw := range list {
			name, ok := nameRaw.(string)
			if !ok {
				continue
			}
			nodeID := MakeNodeID(nodeType, name)
			if nodeSet[nodeID] {
				continue
			}
			nodeSet[nodeID] = true
			result.Nodes = append(result.Nodes, Node{
				ID:   nodeID,
				Type: nodeType,
				Name: name,
			})
		}
	}

	// Extract edges from triples array
	triples, _ := obj["triples"].([]interface{})
	for _, tripleRaw := range triples {
		triple, ok := tripleRaw.(map[string]interface{})
		if !ok {
			continue
		}

		src, _ := triple["src"].(string)
		rel, _ := triple["rel"].(string)
		dst, _ := triple["dst"].(string)
		if src == "" || rel == "" || dst == "" {
			continue
		}

		// Resolve node IDs: look up by name in the nodeSet.
		// The source/destination values are names, we need to find their IDs.
		srcID := resolveComponentNodeID(src, nodeSet)
		dstID := resolveComponentNodeID(dst, nodeSet)

		// If we can't find an existing node, create one with inferred type
		if srcID == "" {
			srcID = MakeNodeID("Unknown", src)
			if !nodeSet[srcID] {
				nodeSet[srcID] = true
				result.Nodes = append(result.Nodes, Node{
					ID:   srcID,
					Type: "Unknown",
					Name: src,
				})
			}
		}
		if dstID == "" {
			dstID = MakeNodeID("Unknown", dst)
			if !nodeSet[dstID] {
				nodeSet[dstID] = true
				result.Nodes = append(result.Nodes, Node{
					ID:   dstID,
					Type: "Unknown",
					Name: dst,
				})
			}
		}

		result.Edges = append(result.Edges, Edge{
			SourceID: srcID,
			TargetID: dstID,
			Relation: rel,
		})
	}

	if len(result.Nodes) == 0 {
		result.Confidence = 0.2
	}

	return result, nil
}

// resolveComponentNodeID finds a node ID in the set by matching the name suffix.
// Component names from triples may match node IDs like "pod:my-pod".
func resolveComponentNodeID(name string, nodeSet map[string]bool) string {
	// Direct check — name might already be a full ID
	if nodeSet[name] {
		return name
	}

	// Search for ID ending with ":name"
	suffix := ":" + name
	for id := range nodeSet {
		if strings.HasSuffix(id, suffix) {
			return id
		}
	}
	return ""
}
