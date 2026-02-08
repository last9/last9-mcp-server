package knowledge

import (
	"fmt"
	"sort"
)

// Extractor defines the interface for pattern-specific structured data extractors.
// Each extractor recognizes a specific tool output shape and converts it to
// graph nodes, edges, and statistics.
type Extractor interface {
	// Name returns a human-readable identifier for diagnostics
	Name() string
	// CanHandle returns true if this extractor recognizes the parsed data shape
	CanHandle(parsed interface{}) bool
	// Extract converts the parsed data into graph elements
	Extract(parsed interface{}) (*ExtractionResult, error)
}

// ExtractionResult holds the output of an extraction pass.
type ExtractionResult struct {
	Nodes      []Node      // Graph nodes to ingest
	Edges      []Edge      // Graph edges to ingest
	Stats      []Statistic // Ephemeral metrics to ingest
	Confidence float64     // 0.0..1.0 — how well the extractor matched
	Pattern    string      // Extractor name, for diagnostics
}

// ExtractorRegistry manages a priority-ordered list of extractors.
// First confident match wins (extractors are sufficiently distinct).
type ExtractorRegistry struct {
	extractors []Extractor
}

// NewExtractorRegistry creates a registry with all known extractors pre-registered.
// Extractors are ordered by specificity: most specific patterns first.
func NewExtractorRegistry() *ExtractorRegistry {
	r := &ExtractorRegistry{}
	// Order: most specific CanHandle checks first
	r.Register(&ComponentDiscoveryExtractor{})
	r.Register(&DependencyGraphExtractor{})
	r.Register(&OperationsSummaryExtractor{})
	r.Register(&ServiceSummaryExtractor{})
	r.Register(&PrometheusExtractor{})
	return r
}

// Register adds an extractor to the registry.
func (r *ExtractorRegistry) Register(e Extractor) {
	r.extractors = append(r.extractors, e)
}

// TryExtract iterates extractors in priority order and returns the first
// confident match. Returns nil if no extractor handles the data.
func (r *ExtractorRegistry) TryExtract(parsed interface{}) (*ExtractionResult, error) {
	for _, e := range r.extractors {
		if e.CanHandle(parsed) {
			result, err := e.Extract(parsed)
			if err != nil {
				return nil, fmt.Errorf("extractor %s failed: %w", e.Name(), err)
			}
			result.Pattern = e.Name()
			return result, nil
		}
	}
	return nil, nil
}

// Pipeline orchestrates format detection, extraction, and Drain fallback.
type Pipeline struct {
	registry *ExtractorRegistry
	drain    *DrainTree
}

// NewPipeline creates a pipeline with all extractors registered and a
// fresh DrainTree for plain text fallback.
func NewPipeline() *Pipeline {
	return &Pipeline{
		registry: NewExtractorRegistry(),
		drain:    NewDrainTree(),
	}
}

// Process detects the format of raw text, dispatches to the appropriate
// extractor, and falls back to Drain for plain text log lines.
func (p *Pipeline) Process(rawText string) (*ExtractionResult, error) {
	format, parsed, err := DetectFormat(rawText)
	if err != nil {
		return nil, fmt.Errorf("format detection failed: %w", err)
	}

	switch format {
	case FormatJSON, FormatYAML:
		// Try structural extractors
		result, err := p.registry.TryExtract(parsed)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
		// No extractor matched structured data — return error so the agent
		// can parse it manually
		return nil, fmt.Errorf("structured data detected (%s) but no extractor matched the shape", format)

	case FormatPlainText:
		return p.processDrain(rawText)

	case FormatCSV:
		// CSV not yet supported by extractors — instruct agent to parse
		return nil, fmt.Errorf("CSV format detected but no extractor available; parse the data yourself and retry with structured nodes/edges")

	default:
		return nil, fmt.Errorf("unrecognized input format")
	}
}

// processDrain handles plain text via the existing Drain log template miner.
// Preserves the original Drain behavior including the hardcoded
// "Connection to <*> failed" rule.
func (p *Pipeline) processDrain(text string) (*ExtractionResult, error) {
	clusterID, vars := p.drain.Parse(text)
	if clusterID == "" {
		return nil, fmt.Errorf("no matching template found for plain text input")
	}

	template := p.drain.GetTemplateString(clusterID)

	// Hardcoded rule from original implementation:
	// "Connection to <*> failed" -> FAILED_CONNECTION edge
	if containsTemplate(template, "Connection to <*> failed") && len(vars) > 0 {
		target := vars[0]
		node := Node{ID: "unknown:source", Type: "Unknown"}
		targetNode := Node{ID: target, Type: "Inferred"}
		edge := Edge{SourceID: node.ID, TargetID: targetNode.ID, Relation: "FAILED_CONNECTION"}
		return &ExtractionResult{
			Nodes:      []Node{node, targetNode},
			Edges:      []Edge{edge},
			Confidence: 0.5,
			Pattern:    "drain",
		}, nil
	}

	return nil, fmt.Errorf("log matched template '%s' but no mapping rule exists", template)
}

func containsTemplate(template, pattern string) bool {
	return len(template) >= len(pattern) && contains(template, pattern)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// MatchSchemasScored scores extracted nodes/edges against all schemas using
// a weighted multi-dimensional formula:
//   - 0.5 * EdgeCoverage (how many input edge triples exist in the schema)
//   - 0.3 * NodeCoverage (how many input node types exist in the schema)
//   - 0.2 * FieldConfidence (avg best token similarity of input node types vs schema nodes)
//
// Threshold for match: 0.6 (lowered from 0.8 since field confidence fills naming gaps).
func MatchSchemasScored(inputNodes []Node, inputEdges []Edge, schemas []Schema) []SchemaMatch {
	if len(inputNodes) == 0 && len(inputEdges) == 0 {
		return nil
	}

	// Collect unique input node types
	inputNodeTypes := make(map[string]bool)
	for _, n := range inputNodes {
		inputNodeTypes[n.Type] = true
	}

	inputSig := computeSignature(inputNodes, inputEdges)

	var matches []SchemaMatch

	for _, schema := range schemas {
		schemaSig := schemaSignature(schema)

		// Edge coverage: how many input triples are in the schema
		edgeCoverage := 0.0
		if len(inputSig) > 0 {
			intersectionCount := 0
			for t := range inputSig {
				if schemaSig[t] {
					intersectionCount++
				}
			}
			edgeCoverage = float64(intersectionCount) / float64(len(inputSig))
		}

		// Node coverage: how many input node types match schema node types
		schemaNodeSet := make(map[string]bool)
		for _, n := range schema.Blueprint.Nodes {
			schemaNodeSet[n] = true
		}

		nodeCoverage := 0.0
		if len(inputNodeTypes) > 0 {
			matchedNodes := 0
			for nt := range inputNodeTypes {
				if schemaNodeSet[nt] {
					matchedNodes++
				}
			}
			nodeCoverage = float64(matchedNodes) / float64(len(inputNodeTypes))
		}

		// Field confidence: avg best token similarity of each input node type
		// against schema node types
		fieldConfidence := 0.0
		if len(inputNodeTypes) > 0 {
			totalSim := 0.0
			for nt := range inputNodeTypes {
				bestSim := 0.0
				for _, snt := range schema.Blueprint.Nodes {
					sim := TokenSimilarity(nt, snt)
					if sim > bestSim {
						bestSim = sim
					}
				}
				totalSim += bestSim
			}
			fieldConfidence = totalSim / float64(len(inputNodeTypes))
		}

		score := 0.5*edgeCoverage + 0.3*nodeCoverage + 0.2*fieldConfidence

		if score >= 0.6 {
			matches = append(matches, SchemaMatch{
				Name:            schema.Name,
				Score:           score,
				NodeCoverage:    nodeCoverage,
				EdgeCoverage:    edgeCoverage,
				FieldConfidence: fieldConfidence,
			})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	return matches
}

// SchemaMatch represents a scored schema match result.
type SchemaMatch struct {
	Name            string  `json:"name"`
	Score           float64 `json:"score"`
	NodeCoverage    float64 `json:"node_coverage"`
	EdgeCoverage    float64 `json:"edge_coverage"`
	FieldConfidence float64 `json:"field_confidence"`
}
