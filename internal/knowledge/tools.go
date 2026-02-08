package knowledge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool description constants
const (
	ListKnowledgeSchemasDescription = `List all registered knowledge schemas (both builtin and user-defined).
Returns schema name, description, builtin status, blueprint (node types and edge patterns), and associated services.
Use this to understand what architectural patterns are available before ingesting data or associating services.`

	AddServiceToSchemaDescription = `Associate a service with an existing knowledge schema.
This tells the system that a particular service follows the given architectural pattern.
The service name is added to the schema's scope, enabling schema-aware queries and topology lookups.
Example: add_service_to_schema(schema_name="http_k8s_datastore", service="payment-api")`

	RemoveServiceFromSchemaDescription = `Remove a service association from a knowledge schema.
This disassociates a service from an architectural pattern without deleting any graph data.
Example: remove_service_from_schema(schema_name="http_k8s_datastore", service="payment-api")`

	AddKnowledgeNoteDescription = `Add a contextual note to the knowledge graph, linked to one or more nodes and/or edges.
Use this to record root cause analyses, ownership info, downtime records, architectural decisions, agent inferences, or any contextual knowledge that enriches the graph.
Notes are searchable and appear in search results as references.

You are strongly encouraged to add notes whenever you discover important context:
- After an RCA: summarize root cause, impact, and remediation
- When you learn ownership: record which team owns a service
- On architectural decisions: document why a pattern was chosen
- On failure modes: describe known failure scenarios and mitigations

The body supports markdown for rich formatting.

Example:
  add_knowledge_note(
    title="RCA: payment-api latency spike 2024-01-15",
    body="## Root Cause\nConnection pool exhaustion on postgres-primary...",
    node_ids=["svc:payment-api", "db:postgres-primary"],
    edge_refs=[{"source": "svc:payment-api", "target": "db:postgres-primary", "relation": "CALLS"}]
  )`

	GetKnowledgeNoteDescription = `Retrieve the full content of a knowledge note by ID.
Use this after seeing a note reference in search results to read the complete markdown body and see all linked entities.`

	DeleteKnowledgeNoteDescription = `Permanently delete a knowledge note by ID.
This removes the note and all its links to nodes and edges. The linked nodes and edges themselves are not affected.`
)

// DefineKnowledgeSchemaArgs arguments for defining a schema
type DefineKnowledgeSchemaArgs struct {
	Name         string          `json:"name"`
	Environments []string        `json:"environments,omitempty"`
	Services     []string        `json:"services,omitempty"`
	Blueprint    SchemaBlueprint `json:"blueprint"`
}

// ListSchemasArgs arguments for listing schemas (no args needed)
type ListSchemasArgs struct{}

// AddServiceToSchemaArgs arguments for adding a service to a schema
type AddServiceToSchemaArgs struct {
	SchemaName string `json:"schema_name"`
	Service    string `json:"service"`
}

// RemoveServiceFromSchemaArgs arguments for removing a service from a schema
type RemoveServiceFromSchemaArgs struct {
	SchemaName string `json:"schema_name"`
	Service    string `json:"service"`
}

// IngestKnowledgeArgs arguments for ingesting data
type IngestKnowledgeArgs struct {
	Nodes   []Node      `json:"nodes,omitempty"`
	Edges   []Edge      `json:"edges,omitempty"`
	Stats   []Statistic `json:"stats,omitempty"`
	Events  []Event     `json:"events,omitempty"`
	RawText string      `json:"raw_text,omitempty"`
}

// SearchKnowledgeGraphArgs arguments for search
type SearchKnowledgeGraphArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// DefineKnowledgeSchemaHandler impl
// Rejects modifications to builtin schemas to protect standard architectural patterns.
func NewDefineSchemaHandler(store Store) func(context.Context, *mcp.CallToolRequest, DefineKnowledgeSchemaArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args DefineKnowledgeSchemaArgs) (*mcp.CallToolResult, any, error) {
		if args.Name == "" {
			return nil, nil, fmt.Errorf("schema name is required")
		}

		// Check if schema is builtin - reject modifications
		existing, err := store.GetSchema(ctx, args.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to check existing schema: %w", err)
		}
		if existing != nil && existing.Builtin {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Schema '%s' is a builtin schema and cannot be modified. Use add_service_to_schema/remove_service_from_schema to manage service associations.", args.Name)},
				},
			}, nil, nil
		}

		schema := Schema{
			Name:         args.Name,
			Blueprint:    args.Blueprint,
			Environments: args.Environments,
			Services:     args.Services,
		}

		if err := store.RegisterSchema(ctx, schema); err != nil {
			return nil, nil, fmt.Errorf("failed to register schema: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Schema '%s' registered successfully.", args.Name)},
			},
		}, nil, nil
	}
}

// IngestKnowledgeHandler impl.
// Uses Pipeline for format-aware extraction from raw_text (JSON/YAML/CSV/PlainText),
// falling back to Drain for unstructured log lines.
func NewIngestHandler(store Store, pipeline *Pipeline) func(context.Context, *mcp.CallToolRequest, IngestKnowledgeArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args IngestKnowledgeArgs) (*mcp.CallToolResult, any, error) {

		// 1. Parsing Logic
		// If raw_text is provided, run it through the extraction pipeline
		if args.RawText != "" {
			result, err := pipeline.Process(args.RawText)
			if err != nil {
				msg := fmt.Sprintf("ParsingError: %s. Please analyze the input strictly, extract the graph data yourself, and call this tool again with the data formatted as a JSON object: { \"nodes\": [...], \"edges\": [...] }", err.Error())
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{
						&mcp.TextContent{Text: msg},
					},
				}, nil, nil
			}
			args.Nodes = append(args.Nodes, result.Nodes...)
			args.Edges = append(args.Edges, result.Edges...)
			args.Stats = append(args.Stats, result.Stats...)
		}

		// 2. Schema Matching (scored)
		schemas, err := store.ListSchemas(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list schemas: %w", err)
		}

		scoredMatches := MatchSchemasScored(args.Nodes, args.Edges, schemas)

		// 3. Ingestion
		if len(args.Nodes) > 0 {
			if err := store.IngestNodes(ctx, args.Nodes); err != nil {
				return nil, nil, fmt.Errorf("node ingestion failed: %w", err)
			}
		}
		if len(args.Edges) > 0 {
			if err := store.IngestEdges(ctx, args.Edges); err != nil {
				return nil, nil, fmt.Errorf("edge ingestion failed: %w", err)
			}
		}
		if len(args.Stats) > 0 {
			if err := store.IngestStatistics(ctx, args.Stats); err != nil {
				return nil, nil, fmt.Errorf("stats ingestion failed: %w", err)
			}
		}
		if len(args.Events) > 0 {
			if err := store.IngestEvents(ctx, args.Events); err != nil {
				return nil, nil, fmt.Errorf("event ingestion failed: %w", err)
			}
		}

		resp := fmt.Sprintf("Successfully ingested %d nodes, %d edges, %d stats, %d events.", len(args.Nodes), len(args.Edges), len(args.Stats), len(args.Events))
		if len(scoredMatches) > 0 {
			names := make([]string, len(scoredMatches))
			for i, m := range scoredMatches {
				names[i] = fmt.Sprintf("%s (score=%.2f)", m.Name, m.Score)
			}
			resp += fmt.Sprintf("\nMatched Architectural Schemas: %v", names)
		} else {
			resp += "\nNo specific Schema matched."
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: resp},
			},
		}, nil, nil
	}
}

// NewListSchemasHandler returns all schemas as JSON
func NewListSchemasHandler(store Store) func(context.Context, *mcp.CallToolRequest, ListSchemasArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ListSchemasArgs) (*mcp.CallToolResult, any, error) {
		schemas, err := store.ListSchemas(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list schemas: %w", err)
		}

		bytes, _ := json.MarshalIndent(schemas, "", "  ")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(bytes)},
			},
		}, nil, nil
	}
}

// NewAddServiceToSchemaHandler adds a service to a schema's scope with deduplication
func NewAddServiceToSchemaHandler(store Store) func(context.Context, *mcp.CallToolRequest, AddServiceToSchemaArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args AddServiceToSchemaArgs) (*mcp.CallToolResult, any, error) {
		if args.SchemaName == "" {
			return nil, nil, fmt.Errorf("schema_name is required")
		}
		if args.Service == "" {
			return nil, nil, fmt.Errorf("service is required")
		}

		schema, err := store.GetSchema(ctx, args.SchemaName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get schema: %w", err)
		}
		if schema == nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Schema '%s' not found.", args.SchemaName)},
				},
			}, nil, nil
		}

		// Deduplicate: check if service already exists
		for _, s := range schema.Services {
			if s == args.Service {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: fmt.Sprintf("Service '%s' is already associated with schema '%s'.", args.Service, args.SchemaName)},
					},
				}, nil, nil
			}
		}

		// Remove wildcard if present when adding a specific service
		services := make([]string, 0, len(schema.Services)+1)
		for _, s := range schema.Services {
			if s != "*" {
				services = append(services, s)
			}
		}
		services = append(services, args.Service)

		if err := store.UpdateSchemaServices(ctx, args.SchemaName, services); err != nil {
			return nil, nil, fmt.Errorf("failed to update schema services: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Service '%s' added to schema '%s'.", args.Service, args.SchemaName)},
			},
		}, nil, nil
	}
}

// NewRemoveServiceFromSchemaHandler removes a service from a schema's scope
func NewRemoveServiceFromSchemaHandler(store Store) func(context.Context, *mcp.CallToolRequest, RemoveServiceFromSchemaArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args RemoveServiceFromSchemaArgs) (*mcp.CallToolResult, any, error) {
		if args.SchemaName == "" {
			return nil, nil, fmt.Errorf("schema_name is required")
		}
		if args.Service == "" {
			return nil, nil, fmt.Errorf("service is required")
		}

		schema, err := store.GetSchema(ctx, args.SchemaName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get schema: %w", err)
		}
		if schema == nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Schema '%s' not found.", args.SchemaName)},
				},
			}, nil, nil
		}

		found := false
		services := make([]string, 0, len(schema.Services))
		for _, s := range schema.Services {
			if s == args.Service {
				found = true
				continue
			}
			services = append(services, s)
		}

		if !found {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Service '%s' is not associated with schema '%s'.", args.Service, args.SchemaName)},
				},
			}, nil, nil
		}

		if err := store.UpdateSchemaServices(ctx, args.SchemaName, services); err != nil {
			return nil, nil, fmt.Errorf("failed to update schema services: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Service '%s' removed from schema '%s'.", args.Service, args.SchemaName)},
			},
		}, nil, nil
	}
}

// AddNoteArgs arguments for adding a note
type AddNoteArgs struct {
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	NodeIDs  []string  `json:"node_ids,omitempty"`
	EdgeRefs []EdgeRef `json:"edge_refs,omitempty"`
}

// GetNoteArgs arguments for retrieving a note
type GetNoteArgs struct {
	ID string `json:"id"`
}

// DeleteNoteArgs arguments for deleting a note
type DeleteNoteArgs struct {
	ID string `json:"id"`
}

// NewAddNoteHandler creates a note linked to specified nodes and/or edges.
func NewAddNoteHandler(store Store) func(context.Context, *mcp.CallToolRequest, AddNoteArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args AddNoteArgs) (*mcp.CallToolResult, any, error) {
		if args.Title == "" {
			return nil, nil, fmt.Errorf("title is required")
		}
		if args.Body == "" {
			return nil, nil, fmt.Errorf("body is required")
		}
		if len(args.NodeIDs) == 0 && len(args.EdgeRefs) == 0 {
			return nil, nil, fmt.Errorf("at least one node_id or edge_ref is required")
		}

		note, err := store.CreateNote(ctx, args.Title, args.Body, args.NodeIDs, args.EdgeRefs)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create note: %w", err)
		}

		bytes, _ := json.MarshalIndent(note, "", "  ")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(bytes)},
			},
		}, nil, nil
	}
}

// NewGetNoteHandler retrieves a note by ID with its linked entities.
func NewGetNoteHandler(store Store) func(context.Context, *mcp.CallToolRequest, GetNoteArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetNoteArgs) (*mcp.CallToolResult, any, error) {
		if args.ID == "" {
			return nil, nil, fmt.Errorf("id is required")
		}

		note, nodeIDs, edgeRefs, err := store.GetNote(ctx, args.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get note: %w", err)
		}
		if note == nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Note '%s' not found.", args.ID)},
				},
			}, nil, nil
		}

		resp := struct {
			*Note
			LinkedNodes []string  `json:"linked_nodes"`
			LinkedEdges []EdgeRef `json:"linked_edges"`
		}{
			Note:        note,
			LinkedNodes: nodeIDs,
			LinkedEdges: edgeRefs,
		}

		bytes, _ := json.MarshalIndent(resp, "", "  ")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(bytes)},
			},
		}, nil, nil
	}
}

// NewDeleteNoteHandler removes a note by ID.
func NewDeleteNoteHandler(store Store) func(context.Context, *mcp.CallToolRequest, DeleteNoteArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args DeleteNoteArgs) (*mcp.CallToolResult, any, error) {
		if args.ID == "" {
			return nil, nil, fmt.Errorf("id is required")
		}

		if err := store.DeleteNote(ctx, args.ID); err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Note '%s' not found.", args.ID)},
				},
			}, nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Note '%s' deleted.", args.ID)},
			},
		}, nil, nil
	}
}

// SearchHandler impl
func NewSearchHandler(store Store) func(context.Context, *mcp.CallToolRequest, SearchKnowledgeGraphArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args SearchKnowledgeGraphArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		}

		res, err := store.Search(ctx, args.Query, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("search failed: %w", err)
		}

		// Format as JSON for the Agent to read easily
		bytes, _ := json.MarshalIndent(res, "", "  ")

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(bytes)},
			},
		}, nil, nil
	}
}
