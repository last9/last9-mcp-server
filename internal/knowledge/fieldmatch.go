package knowledge

import (
	"fmt"
	"strings"
	"unicode"
)

// FieldAliases maps common field names from tool outputs to schema node types.
// Used as a fallback when token similarity alone is insufficient.
var FieldAliases = map[string]string{
	"service_name": "Service",
	"ServiceName":  "Service",
	"svc":          "Service",
	"service":      "Service",

	"db_system":  "DataStoreInstance",
	"database":   "DataStoreInstance",
	"db":         "DataStoreInstance",
	"datastore":  "DataStoreInstance",
	"databases":  "DataStoreInstance",

	"pod":        "Pod",
	"container":  "Container",
	"namespace":  "Namespace",
	"deployment": "Deployment",

	"endpoint":         "HTTPEndpoint",
	"operation":        "HTTPEndpoint",
	"operation_name":   "HTTPEndpoint",
	"span_name":        "HTTPEndpoint",

	"messaging_system": "KafkaTopic",
	"kafka_topic":      "KafkaTopic",
}

// TokenizeName splits compound names into lowercase tokens.
// Handles camelCase, PascalCase, snake_case, and kebab-case.
//
// Examples:
//   - "ServiceName"  -> ["service", "name"]
//   - "db_system"    -> ["db", "system"]
//   - "net-peer-name" -> ["net", "peer", "name"]
//   - "HTTPEndpoint" -> ["http", "endpoint"]
func TokenizeName(name string) []string {
	// Replace separators with spaces
	normalized := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(name)

	// Split camelCase/PascalCase
	var parts []string
	current := strings.Builder{}
	runes := []rune(normalized)

	for i, r := range runes {
		if r == ' ' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		if unicode.IsUpper(r) && current.Len() > 0 {
			// Start new token on Upper following lower: "serviceName" -> "service", "Name"
			// But keep consecutive uppers together: "HTTP" stays as one token until
			// we see an upper followed by lower: "HTTPEndpoint" -> "HTTP", "Endpoint"
			if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				parts = append(parts, current.String())
				current.Reset()
			} else if i > 0 && unicode.IsLower(runes[i-1]) {
				parts = append(parts, current.String())
				current.Reset()
			}
		}
		current.WriteRune(unicode.ToLower(r))
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// TokenSimilarity returns the Jaccard similarity (0.0..1.0) between two names
// after tokenizing them. This captures compound-word overlap that edit distance misses.
//
// Example: TokenSimilarity("service_name", "ServiceName") = 1.0
func TokenSimilarity(a, b string) float64 {
	tokensA := TokenizeName(a)
	tokensB := TokenizeName(b)

	if len(tokensA) == 0 && len(tokensB) == 0 {
		return 1.0
	}
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return 0.0
	}

	setA := make(map[string]bool, len(tokensA))
	for _, t := range tokensA {
		setA[t] = true
	}
	setB := make(map[string]bool, len(tokensB))
	for _, t := range tokensB {
		setB[t] = true
	}

	// Intersection
	intersection := 0
	for t := range setA {
		if setB[t] {
			intersection++
		}
	}

	// Union
	union := make(map[string]bool, len(setA)+len(setB))
	for t := range setA {
		union[t] = true
	}
	for t := range setB {
		union[t] = true
	}

	return float64(intersection) / float64(len(union))
}

// MakeNodeID generates a deterministic, type-prefixed node identifier.
// Cross-tool merging relies on the same entity getting the same ID.
//
// Examples:
//   - MakeNodeID("Service", "frontend") -> "service:frontend"
//   - MakeNodeID("DataStoreInstance", "mysql", "db-host") -> "datastoreinstance:mysql:db-host"
func MakeNodeID(nodeType string, parts ...string) string {
	prefix := strings.ToLower(nodeType)
	if len(parts) == 0 {
		return prefix
	}
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return prefix
	}
	return fmt.Sprintf("%s:%s", prefix, strings.Join(cleaned, ":"))
}

// ResolveNodeType tries to map a field name to a schema node type.
// First checks FieldAliases for an exact match, then uses token similarity
// against known schema node types. Returns empty string if no match found.
func ResolveNodeType(fieldName string, schemaNodeTypes []string) string {
	// Exact alias match
	if nodeType, ok := FieldAliases[fieldName]; ok {
		return nodeType
	}

	// Token similarity against schema nodes
	bestScore := 0.0
	bestType := ""
	for _, nt := range schemaNodeTypes {
		score := TokenSimilarity(fieldName, nt)
		if score > bestScore {
			bestScore = score
			bestType = nt
		}
	}

	if bestScore >= 0.5 {
		return bestType
	}
	return ""
}
