package knowledge

import "time"

// Node represents an entity in the graph
type Node struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Name       string                 `json:"name,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	UpdatedAt  time.Time              `json:"updated_at,omitempty"`
}

// Edge represents a relationship between nodes
type Edge struct {
	SourceID   string                 `json:"source"`
	TargetID   string                 `json:"target"`
	Relation   string                 `json:"relation"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	UpdatedAt  time.Time              `json:"updated_at,omitempty"`
}

// Statistic represents an ephemeral metric for a node
type Statistic struct {
	NodeID     string    `json:"node_id"`
	MetricName string    `json:"metric_name"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Event represents a semantic occurrence (aggregated)
type Event struct {
	SourceID        string                 `json:"source"`
	TargetID        string                 `json:"target,omitempty"`
	Type            string                 `json:"type"`
	Status          string                 `json:"status,omitempty"`
	Severity        string                 `json:"severity"`         // info, warn, error, fatal
	TimeWindowStart time.Time              `json:"window_start"`     // Start of bucket
	TimeWindowEnd   time.Time              `json:"window_end"`       // End of bucket
	Timestamp       time.Time              `json:"recent_timestamp"` // Actual last occurrence
	Count           int                    `json:"count"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// FieldHint provides alias and ID-field hints for a schema node type,
// helping the extraction pipeline map tool output fields to graph types.
type FieldHint struct {
	Aliases  []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	IDFields []string `json:"id_fields,omitempty" yaml:"id_fields,omitempty"`
}

// Schema Blueprint Definition
type SchemaBlueprint struct {
	Nodes      []string             `json:"nodes" yaml:"nodes"`                                 // List of allowed node types
	Edges      []string             `json:"edges" yaml:"edges"`                                 // List of allowed edge patterns "Source -> RELATION -> Target"
	FieldHints map[string]FieldHint `json:"field_hints,omitempty" yaml:"field_hints,omitempty"` // Optional per-node-type hints
}

// Schema wraps the blueprint with metadata and scope
type Schema struct {
	Name         string          `json:"name" yaml:"name"`
	Description  string          `json:"description,omitempty" yaml:"description"`
	Builtin      bool            `json:"builtin,omitempty" yaml:"builtin"`
	Blueprint    SchemaBlueprint `json:"blueprint" yaml:"blueprint"`
	Environments []string        `json:"environments" yaml:"environments"` // ["prod", "staging"] or ["*"]
	Services     []string        `json:"services" yaml:"services"`         // ["payment-*", "auth"] or ["*"]
}

// Note represents a contextual annotation linked to graph entities.
type Note struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// NoteRef is a lightweight reference returned in search results.
type NoteRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// EdgeRef identifies an edge by its composite natural key.
type EdgeRef struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
}

type SearchResult struct {
	Nodes  []Node      `json:"nodes"`
	Edges  []Edge      `json:"edges"`
	Stats  []Statistic `json:"stats"`
	Events []Event     `json:"events"`
	Notes  []NoteRef   `json:"notes"`
}

type Topology struct {
	RootID string `json:"root_id"`
	Edges  []Edge `json:"edges"`
}
