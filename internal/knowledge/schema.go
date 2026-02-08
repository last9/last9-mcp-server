package knowledge

import (
	"strings"
)

// StructuralTriple represents a generic relationship pattern (Type(Source) -> Rel -> Type(Target))
type StructuralTriple struct {
	SourceType string
	Relation   string
	TargetType string
}

// computeSignature extracts the set of unique structural triples from a subgraph
func computeSignature(nodes []Node, edges []Edge) map[StructuralTriple]bool {
	sig := make(map[StructuralTriple]bool)
	nodeTypes := make(map[string]string)

	for _, n := range nodes {
		nodeTypes[n.ID] = n.Type
	}

	for _, e := range edges {
		srcType, okSrc := nodeTypes[e.SourceID]
		tgtType, okTgt := nodeTypes[e.TargetID]

		// If either node is missing from the ingestion batch, we might not know its type.
		// In a real system, we'd query the DB for existing nodes.
		// For now, we only compute signature based on the provided batch context.
		if okSrc && okTgt {
			triple := StructuralTriple{
				SourceType: srcType,
				Relation:   e.Relation,
				TargetType: tgtType,
			}
			sig[triple] = true
		}
	}
	return sig
}

// schemaSignature pre-computes the signature for a schema definition
func schemaSignature(s Schema) map[StructuralTriple]bool {
	sig := make(map[StructuralTriple]bool)
	for _, edgeDef := range s.Blueprint.Edges {
		// format: "SourceType -> RELATION -> TargetType"
		parts := strings.Split(edgeDef, "->")
		if len(parts) == 3 {
			triple := StructuralTriple{
				SourceType: strings.TrimSpace(parts[0]),
				Relation:   strings.TrimSpace(parts[1]),
				TargetType: strings.TrimSpace(parts[2]),
			}
			sig[triple] = true
		}
	}
	return sig
}

// MatchSchemas finds which schemas the input subgraph adheres to
func MatchSchemas(inputNodes []Node, inputEdges []Edge, activeSchemas []Schema) []string {
	if len(inputEdges) == 0 {
		return []string{}
	}

	inputSig := computeSignature(inputNodes, inputEdges)
	if len(inputSig) == 0 {
		return []string{}
	}

	var matches []string

	for _, schema := range activeSchemas {
		schemaSig := schemaSignature(schema)

		// Calculate Intersection
		intersectionCount := 0
		for t := range inputSig {
			if schemaSig[t] {
				intersectionCount++
			}
		}

		// Calculate Score: How much of the Input is explained by the Schema?
		score := float64(intersectionCount) / float64(len(inputSig))

		// Threshold for "Partial Match", but aiming for 1.0 (Perfect Match) usually
		if score >= 0.8 {
			matches = append(matches, schema.Name)
		}
	}

	return matches
}
