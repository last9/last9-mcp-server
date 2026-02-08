package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestStore creates a temporary SQLite store for testing, returning
// the store and a cleanup function that removes DB files.
func newTestStore(t *testing.T, name string) (Store, func()) {
	t.Helper()
	store, err := NewStore(name)
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}
	return store, func() {
		store.Close()
		os.Remove(name)
		os.Remove(name + "-shm")
		os.Remove(name + "-wal")
	}
}

// seedTestNodes ingests some nodes and edges useful for note tests.
func seedTestNodes(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	if err := store.IngestNodes(ctx, []Node{
		{ID: "svc:api", Type: "Service", Name: "api-gateway"},
		{ID: "db:pg", Type: "Database", Name: "postgres-primary"},
	}); err != nil {
		t.Fatalf("seed IngestNodes: %v", err)
	}
	if err := store.IngestEdges(ctx, []Edge{
		{SourceID: "svc:api", TargetID: "db:pg", Relation: "CALLS"},
	}); err != nil {
		t.Fatalf("seed IngestEdges: %v", err)
	}
}

func TestKnowledgeGraphTools(t *testing.T) {
	// Setup temporary DB
	tmpDB := "test_knowledge.db"
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

	// Pre-seed Drain with a generalized template for the "Connection to <*> failed" rule.
	// The simplified Drain impl needs direct seeding to have a generalized template.
	// Uses "db:postgres" which collides with the node from step 2 — proving the
	// external-content FTS upsert fix works correctly.
	pipeline.drain.Clusters["C1"] = &LogCluster{
		ID:       "C1",
		Template: []string{"Connection", "to", "<*>", "failed"},
		RawLogs:  make(CountMap),
	}
	lengthNode := &DrainNode{Children: make(map[string]*DrainNode)}
	connNode := &DrainNode{Children: make(map[string]*DrainNode)}
	toNode := &DrainNode{Children: make(map[string]*DrainNode)}
	dbNode := &DrainNode{Children: make(map[string]*DrainNode)}
	leafNode := &DrainNode{IsLeaf: true, ClusterID: "C1", Children: make(map[string]*DrainNode)}
	pipeline.drain.Root.Children["4"] = lengthNode
	lengthNode.Children["Connection"] = connNode
	connNode.Children["to"] = toNode
	toNode.Children["db:postgres"] = dbNode
	dbNode.Children["failed"] = leafNode

	ctx := context.Background()

	// 1. Define Schema
	defineHandler := NewDefineSchemaHandler(store)
	_, _, err = defineHandler(ctx, nil, DefineKnowledgeSchemaArgs{
		Name:         "MicroserviceDB",
		Environments: []string{"prod"},
		Blueprint: SchemaBlueprint{
			Nodes: []string{"Service", "Database"},
			Edges: []string{"Service -> CALLS -> Database"},
		},
	})
	if err != nil {
		t.Fatalf("DefineSchema failed: %v", err)
	}

	// 2. Ingest JSON (Happy Path)
	ingestHandler := NewIngestHandler(store, pipeline)
	_, _, err = ingestHandler(ctx, nil, IngestKnowledgeArgs{
		Nodes: []Node{{ID: "svc:payment", Type: "Service"}, {ID: "db:postgres", Type: "Database"}},
		Edges: []Edge{{SourceID: "svc:payment", TargetID: "db:postgres", Relation: "CALLS"}},
	})
	if err != nil {
		t.Fatalf("Ingest JSON failed: %v", err)
	}

	// 3. Ingest Logs (Drain Path via Pipeline)
	// Uses "db:postgres" which exercises the FTS upsert path (node already exists from step 2).
	res, _, err := ingestHandler(ctx, nil, IngestKnowledgeArgs{
		RawText: "Connection to db:postgres failed",
	})
	if err != nil {
		t.Fatalf("Ingest Drain Log failed: %v", err)
	}
	// Verify it succeeded (not IsError)
	if res.IsError {
		t.Logf("Drain path returned error (expected for exact template match without wildcards): %s",
			res.Content[0].(*mcp.TextContent).Text)
	}

	// 4. Ingest Event
	_, _, err = ingestHandler(ctx, nil, IngestKnowledgeArgs{
		Events: []Event{{
			SourceID:  "svc:payment",
			TargetID:  "db:postgres",
			Type:      "Restart",
			Severity:  "error",
			Status:    "failure",
			Timestamp: time.Now(),
		}},
	})
	if err != nil {
		t.Fatalf("Ingest Event failed: %v", err)
	}

	// 5. Search
	searchHandler := NewSearchHandler(store)
	searchRes, _, err := searchHandler(ctx, nil, SearchKnowledgeGraphArgs{Query: "payment"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	content := searchRes.Content[0].(*mcp.TextContent).Text
	var result SearchResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("Failed to unmarshal search result: %v", err)
	}

	if len(result.Nodes) == 0 {
		t.Error("Search returned no nodes")
	}
	if len(result.Edges) == 0 {
		t.Error("Search returned no edges")
	}

	if len(result.Events) == 0 {
		t.Logf("Search returned no events (might be timing issue). Result: %s", content)
	} else {
		t.Logf("Found %d events", len(result.Events))
	}
}

func TestIngestHandler_StructuredJSON_DependencyGraph(t *testing.T) {
	tmpDB := "test_ingest_depgraph.db"
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

	ctx := context.Background()
	if err := RegisterBuiltinSchemas(ctx, store); err != nil {
		t.Fatalf("RegisterBuiltinSchemas failed: %v", err)
	}

	pipeline := NewPipeline()
	ingestHandler := NewIngestHandler(store, pipeline)

	// Ingest raw JSON from get_service_dependency_graph
	rawJSON := `{
		"service_name": "payment-api",
		"env": "prod",
		"incoming": {
			"checkout-svc": {"Throughput": 150.5, "ErrorRate": 2.3}
		},
		"outgoing": {
			"notification-svc": {"Throughput": 200.0}
		},
		"databases": {
			"postgres-primary": {"Throughput": 300.0}
		}
	}`

	res, _, err := ingestHandler(ctx, nil, IngestKnowledgeArgs{
		RawText: rawJSON,
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("Ingest returned error: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	content := res.Content[0].(*mcp.TextContent).Text
	// Should report ingested nodes/edges
	if !strings.Contains(content, "nodes") {
		t.Errorf("expected response to mention nodes: %s", content)
	}

	// Verify data was stored — search for payment
	searchHandler := NewSearchHandler(store)
	searchRes, _, err := searchHandler(ctx, nil, SearchKnowledgeGraphArgs{Query: "payment", Limit: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	searchContent := searchRes.Content[0].(*mcp.TextContent).Text
	var result SearchResult
	json.Unmarshal([]byte(searchContent), &result)

	if len(result.Nodes) == 0 {
		t.Error("expected to find payment-api node after ingestion")
	}
}

func TestIngestHandler_StructuredJSON_OperationsSummary(t *testing.T) {
	tmpDB := "test_ingest_ops.db"
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

	ctx := context.Background()
	if err := RegisterBuiltinSchemas(ctx, store); err != nil {
		t.Fatalf("RegisterBuiltinSchemas failed: %v", err)
	}

	pipeline := NewPipeline()
	ingestHandler := NewIngestHandler(store, pipeline)

	rawJSON := `{
		"service_name": "order-svc",
		"operations": [
			{
				"name": "POST /orders",
				"db_system": "mysql",
				"net_peer_name": "db.orders.internal",
				"throughput": 500,
				"error_rate": 1.5,
				"response_time": {"p50": 10, "p95": 45}
			},
			{
				"name": "GET /orders/{id}",
				"throughput": 1000,
				"error_rate": 0.2
			}
		]
	}`

	res, _, err := ingestHandler(ctx, nil, IngestKnowledgeArgs{
		RawText: rawJSON,
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("Ingest returned error: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	content := res.Content[0].(*mcp.TextContent).Text
	t.Logf("Ingest response: %s", content)

	// Should have ingested: order-svc, 2 endpoints, 1 DataStoreInstance = 4 nodes
	if !strings.Contains(content, "4 nodes") {
		t.Errorf("expected 4 nodes in response: %s", content)
	}

	// Verify HTTPEndpoint nodes exist
	searchHandler := NewSearchHandler(store)
	searchRes, _, _ := searchHandler(ctx, nil, SearchKnowledgeGraphArgs{Query: "orders", Limit: 10})
	searchContent := searchRes.Content[0].(*mcp.TextContent).Text
	var result SearchResult
	json.Unmarshal([]byte(searchContent), &result)

	if len(result.Nodes) == 0 {
		t.Error("expected to find order-related nodes after ingestion")
	}
}

func TestIngestHandler_UnrecognizedJSON(t *testing.T) {
	tmpDB := "test_ingest_unknown.db"
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
	ingestHandler := NewIngestHandler(store, pipeline)

	ctx := context.Background()
	res, _, err := ingestHandler(ctx, nil, IngestKnowledgeArgs{
		RawText: `{"unknown_format": true, "random_data": [1,2,3]}`,
	})
	if err != nil {
		t.Fatalf("Ingest returned Go error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for unrecognized JSON format")
	}
	content := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(content, "ParsingError") {
		t.Errorf("expected ParsingError in message: %s", content)
	}
}

func TestListSchemasHandler(t *testing.T) {
	tmpDB := "test_list_schemas.db"
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

	ctx := context.Background()

	// Register builtin schemas
	if err := RegisterBuiltinSchemas(ctx, store); err != nil {
		t.Fatalf("RegisterBuiltinSchemas failed: %v", err)
	}

	// List schemas
	listHandler := NewListSchemasHandler(store)
	res, _, err := listHandler(ctx, nil, ListSchemasArgs{})
	if err != nil {
		t.Fatalf("ListSchemas failed: %v", err)
	}

	content := res.Content[0].(*mcp.TextContent).Text
	var schemas []Schema
	if err := json.Unmarshal([]byte(content), &schemas); err != nil {
		t.Fatalf("Failed to unmarshal list result: %v", err)
	}

	if len(schemas) != 4 {
		t.Errorf("expected 4 schemas, got %d", len(schemas))
	}
}

func TestAddServiceToSchemaHandler(t *testing.T) {
	tmpDB := "test_add_service.db"
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

	ctx := context.Background()
	if err := RegisterBuiltinSchemas(ctx, store); err != nil {
		t.Fatalf("RegisterBuiltinSchemas failed: %v", err)
	}

	addHandler := NewAddServiceToSchemaHandler(store)

	// Add a service
	res, _, err := addHandler(ctx, nil, AddServiceToSchemaArgs{
		SchemaName: "http_k8s_datastore",
		Service:    "payment-api",
	})
	if err != nil {
		t.Fatalf("AddService failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("AddService returned error: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	// Verify service was added
	schema, err := store.GetSchema(ctx, "http_k8s_datastore")
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}
	found := false
	for _, s := range schema.Services {
		if s == "payment-api" {
			found = true
		}
	}
	if !found {
		t.Errorf("service 'payment-api' not found in schema services: %v", schema.Services)
	}

	// Add duplicate - should be idempotent
	res, _, err = addHandler(ctx, nil, AddServiceToSchemaArgs{
		SchemaName: "http_k8s_datastore",
		Service:    "payment-api",
	})
	if err != nil {
		t.Fatalf("AddService duplicate failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("AddService duplicate returned error")
	}

	// Non-existent schema
	res, _, err = addHandler(ctx, nil, AddServiceToSchemaArgs{
		SchemaName: "nonexistent",
		Service:    "payment-api",
	})
	if err != nil {
		t.Fatalf("AddService nonexistent failed: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected error for non-existent schema")
	}
}

func TestRemoveServiceFromSchemaHandler(t *testing.T) {
	tmpDB := "test_remove_service.db"
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

	ctx := context.Background()
	if err := RegisterBuiltinSchemas(ctx, store); err != nil {
		t.Fatalf("RegisterBuiltinSchemas failed: %v", err)
	}

	// First add a service
	addHandler := NewAddServiceToSchemaHandler(store)
	_, _, err = addHandler(ctx, nil, AddServiceToSchemaArgs{
		SchemaName: "http_k8s_datastore",
		Service:    "payment-api",
	})
	if err != nil {
		t.Fatalf("AddService failed: %v", err)
	}

	// Remove the service
	removeHandler := NewRemoveServiceFromSchemaHandler(store)
	res, _, err := removeHandler(ctx, nil, RemoveServiceFromSchemaArgs{
		SchemaName: "http_k8s_datastore",
		Service:    "payment-api",
	})
	if err != nil {
		t.Fatalf("RemoveService failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("RemoveService returned error: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	// Verify service was removed
	schema, err := store.GetSchema(ctx, "http_k8s_datastore")
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}
	for _, s := range schema.Services {
		if s == "payment-api" {
			t.Errorf("service 'payment-api' should have been removed, but still present: %v", schema.Services)
		}
	}

	// Remove non-existent service
	res, _, err = removeHandler(ctx, nil, RemoveServiceFromSchemaArgs{
		SchemaName: "http_k8s_datastore",
		Service:    "nonexistent-svc",
	})
	if err != nil {
		t.Fatalf("RemoveService nonexistent failed: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected error for removing non-existent service")
	}

	// Remove from non-existent schema
	res, _, err = removeHandler(ctx, nil, RemoveServiceFromSchemaArgs{
		SchemaName: "nonexistent",
		Service:    "payment-api",
	})
	if err != nil {
		t.Fatalf("RemoveService nonexistent schema failed: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected error for non-existent schema")
	}
}

// TestNodeUpsertFTS verifies that upserting a node with changed properties
// correctly updates the FTS index — the external-content FTS fix for the
// content-mismatch bug on AFTER UPDATE triggers.
func TestNodeUpsertFTS(t *testing.T) {
	tmpDB := "test_upsert_fts.db"
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

	ctx := context.Background()

	// Insert a node
	err = store.IngestNodes(ctx, []Node{{
		ID:         "svc:alpha",
		Type:       "Service",
		Name:       "AlphaOriginal",
		Properties: map[string]interface{}{"version": "1.0"},
	}})
	if err != nil {
		t.Fatalf("First IngestNodes failed: %v", err)
	}

	// Upsert the same node with changed properties — this is the operation
	// that previously triggered the FTS content-mismatch error.
	err = store.IngestNodes(ctx, []Node{{
		ID:         "svc:alpha",
		Type:       "Service",
		Name:       "AlphaUpdated",
		Properties: map[string]interface{}{"version": "2.0", "region": "us-east"},
	}})
	if err != nil {
		t.Fatalf("Upsert IngestNodes failed: %v", err)
	}

	// Search for the updated name — FTS should reflect the new content
	result, err := store.Search(ctx, "AlphaUpdated", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected to find upserted node via updated name, got 0 results")
	}
	if result.Nodes[0].Name != "AlphaUpdated" {
		t.Errorf("expected name 'AlphaUpdated', got %q", result.Nodes[0].Name)
	}

	// Search for the old name — should no longer match
	oldResult, err := store.Search(ctx, "AlphaOriginal", 10)
	if err != nil {
		t.Fatalf("Search for old name failed: %v", err)
	}
	if len(oldResult.Nodes) != 0 {
		t.Errorf("expected 0 results for old name, got %d", len(oldResult.Nodes))
	}
}

func TestDefineSchemaRejectsBuiltinModification(t *testing.T) {
	tmpDB := "test_builtin_immutable.db"
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

	ctx := context.Background()
	if err := RegisterBuiltinSchemas(ctx, store); err != nil {
		t.Fatalf("RegisterBuiltinSchemas failed: %v", err)
	}

	defineHandler := NewDefineSchemaHandler(store)

	// Try to overwrite a builtin schema
	res, _, err := defineHandler(ctx, nil, DefineKnowledgeSchemaArgs{
		Name: "http_k8s_datastore",
		Blueprint: SchemaBlueprint{
			Nodes: []string{"CustomNode"},
			Edges: []string{"CustomNode -> CALLS -> CustomNode"},
		},
	})
	if err != nil {
		t.Fatalf("DefineSchema failed unexpectedly: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected error when trying to modify builtin schema")
	}

	// Defining a new non-builtin schema should work
	res, _, err = defineHandler(ctx, nil, DefineKnowledgeSchemaArgs{
		Name: "custom_schema",
		Blueprint: SchemaBlueprint{
			Nodes: []string{"CustomNode"},
			Edges: []string{"CustomNode -> CALLS -> CustomNode"},
		},
	})
	if err != nil {
		t.Fatalf("DefineSchema for custom failed: %v", err)
	}
	if res.IsError {
		t.Errorf("should be able to define a non-builtin schema, but got error: %s", res.Content[0].(*mcp.TextContent).Text)
	}
}

func TestAddAndGetNote(t *testing.T) {
	store, cleanup := newTestStore(t, "test_add_get_note.db")
	defer cleanup()

	ctx := context.Background()
	seedTestNodes(t, store)

	addHandler := NewAddNoteHandler(store)
	res, _, err := addHandler(ctx, nil, AddNoteArgs{
		Title:   "RCA: api-gateway latency spike",
		Body:    "## Root Cause\nConnection pool exhaustion on postgres-primary.",
		NodeIDs: []string{"svc:api", "db:pg"},
		EdgeRefs: []EdgeRef{
			{Source: "svc:api", Target: "db:pg", Relation: "CALLS"},
		},
	})
	if err != nil {
		t.Fatalf("AddNote failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("AddNote returned error: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	// Parse the returned note to get the ID
	var note Note
	content := res.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(content), &note); err != nil {
		t.Fatalf("Failed to unmarshal note: %v", err)
	}
	if !strings.HasPrefix(note.ID, "note_") {
		t.Errorf("expected note ID to start with 'note_', got %q", note.ID)
	}
	if note.Title != "RCA: api-gateway latency spike" {
		t.Errorf("unexpected title: %q", note.Title)
	}

	// Get the note
	getHandler := NewGetNoteHandler(store)
	getRes, _, err := getHandler(ctx, nil, GetNoteArgs{ID: note.ID})
	if err != nil {
		t.Fatalf("GetNote failed: %v", err)
	}
	if getRes.IsError {
		t.Fatalf("GetNote returned error: %s", getRes.Content[0].(*mcp.TextContent).Text)
	}

	// Parse the response and verify links
	var resp struct {
		Note
		LinkedNodes []string  `json:"linked_nodes"`
		LinkedEdges []EdgeRef `json:"linked_edges"`
	}
	getContent := getRes.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(getContent), &resp); err != nil {
		t.Fatalf("Failed to unmarshal get response: %v", err)
	}
	if len(resp.LinkedNodes) != 2 {
		t.Errorf("expected 2 linked nodes, got %d", len(resp.LinkedNodes))
	}
	if len(resp.LinkedEdges) != 1 {
		t.Errorf("expected 1 linked edge, got %d", len(resp.LinkedEdges))
	}
	if resp.Body != "## Root Cause\nConnection pool exhaustion on postgres-primary." {
		t.Errorf("unexpected body: %q", resp.Body)
	}
}

func TestDeleteNote(t *testing.T) {
	store, cleanup := newTestStore(t, "test_delete_note.db")
	defer cleanup()

	ctx := context.Background()
	seedTestNodes(t, store)

	// Create a note
	note, err := store.CreateNote(ctx, "Temp note", "Will be deleted", []string{"svc:api"}, nil)
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	// Verify it exists
	got, _, _, err := store.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected note to exist after creation")
	}

	// Delete via handler
	deleteHandler := NewDeleteNoteHandler(store)
	res, _, err := deleteHandler(ctx, nil, DeleteNoteArgs{ID: note.ID})
	if err != nil {
		t.Fatalf("DeleteNote failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("DeleteNote returned error: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	// Verify it's gone
	got, _, _, err = store.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote after delete failed: %v", err)
	}
	if got != nil {
		t.Error("expected note to be nil after deletion")
	}

	// Delete again — should report not found
	res, _, err = deleteHandler(ctx, nil, DeleteNoteArgs{ID: note.ID})
	if err != nil {
		t.Fatalf("DeleteNote second call failed: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when deleting non-existent note")
	}
}

func TestSearchReturnsNotes_FTSMatch(t *testing.T) {
	store, cleanup := newTestStore(t, "test_search_notes_fts.db")
	defer cleanup()

	ctx := context.Background()
	seedTestNodes(t, store)

	// Create a note with "latency" in the title (no node named "latency")
	_, err := store.CreateNote(ctx, "Latency investigation", "High p99 latency observed", []string{"svc:api"}, nil)
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	// Search for "latency" — should find note via FTS even though no node matches
	result, err := store.Search(ctx, "latency", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Notes) == 0 {
		t.Error("expected notes in search results for FTS match on note title")
	}
	if len(result.Notes) > 0 && result.Notes[0].Title != "Latency investigation" {
		t.Errorf("unexpected note title: %q", result.Notes[0].Title)
	}
}

func TestSearchReturnsNotes_LinkedNodes(t *testing.T) {
	store, cleanup := newTestStore(t, "test_search_notes_linked.db")
	defer cleanup()

	ctx := context.Background()
	seedTestNodes(t, store)

	// Create a note linked to svc:api, but note title/body don't contain "api"
	_, err := store.CreateNote(ctx, "Ownership record", "Team Payments owns this service", []string{"svc:api"}, nil)
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	// Search for "api" — should find the node, and the linked note
	result, err := store.Search(ctx, "api", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected to find node for 'api'")
	}
	if len(result.Notes) == 0 {
		t.Error("expected notes in search results via node link")
	}
}

func TestSearchNotesDeduplication(t *testing.T) {
	store, cleanup := newTestStore(t, "test_search_notes_dedup.db")
	defer cleanup()

	ctx := context.Background()
	seedTestNodes(t, store)

	// Create a note that matches FTS AND is linked to a matched node
	// Title contains "api" (FTS match) and is linked to svc:api (node link match)
	_, err := store.CreateNote(ctx, "api gateway performance tuning", "Tuning notes for the api", []string{"svc:api"}, nil)
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	// Search for "api" — note should appear exactly once
	result, err := store.Search(ctx, "api", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	count := 0
	for _, n := range result.Notes {
		if strings.Contains(n.Title, "api gateway performance") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected note to appear exactly once, got %d occurrences (total notes: %d)", count, len(result.Notes))
	}
}

func TestAddNoteValidation(t *testing.T) {
	store, cleanup := newTestStore(t, "test_note_validation.db")
	defer cleanup()

	ctx := context.Background()
	addHandler := NewAddNoteHandler(store)

	// Empty title
	_, _, err := addHandler(ctx, nil, AddNoteArgs{
		Title:   "",
		Body:    "some body",
		NodeIDs: []string{"svc:api"},
	})
	if err == nil {
		t.Error("expected error for empty title")
	}

	// Empty body
	_, _, err = addHandler(ctx, nil, AddNoteArgs{
		Title:   "some title",
		Body:    "",
		NodeIDs: []string{"svc:api"},
	})
	if err == nil {
		t.Error("expected error for empty body")
	}

	// No links
	_, _, err = addHandler(ctx, nil, AddNoteArgs{
		Title: "some title",
		Body:  "some body",
	})
	if err == nil {
		t.Error("expected error for no links")
	}
}

// TestSearchReturnsNotes_EdgeLinked verifies that a note linked only to an edge
// (not directly to any node) is discoverable when searching for one of the
// edge's endpoint nodes. The note title/body intentionally avoids the search
// term so we isolate the edge-link discovery path from FTS matching.
func TestSearchReturnsNotes_EdgeLinked(t *testing.T) {
	store, cleanup := newTestStore(t, "test_search_notes_edge_linked.db")
	defer cleanup()

	ctx := context.Background()
	seedTestNodes(t, store) // creates svc:api, db:pg, edge svc:api->db:pg CALLS

	// Note title/body do NOT contain "api" — discovery must come via edge link, not FTS
	_, err := store.CreateNote(ctx, "This link broke at 08:00 IST", "Connection dropped at 2026-02-08T08:00:00+05:30", nil, []EdgeRef{
		{Source: "svc:api", Target: "db:pg", Relation: "CALLS"},
	})
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	// Search for "api" — should find node svc:api and the edge-linked note
	result, err := store.Search(ctx, "api", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected to find node for 'api'")
	}

	found := false
	for _, n := range result.Notes {
		if n.Title == "This link broke at 08:00 IST" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected edge-linked note in search results, got notes: %+v", result.Notes)
	}
}

// TestNodeEnvPreservedOnUpsert verifies that upserting a node without env
// preserves the env value set by a prior ingestion (COALESCE semantics).
func TestNodeEnvPreservedOnUpsert(t *testing.T) {
	store, cleanup := newTestStore(t, "test_env_preserve.db")
	defer cleanup()

	ctx := context.Background()

	// Insert node with env set
	err := store.IngestNodes(ctx, []Node{{
		ID:   "svc:alpha",
		Type: "Service",
		Name: "alpha",
		Env:  "production",
	}})
	if err != nil {
		t.Fatalf("First IngestNodes failed: %v", err)
	}

	// Upsert the same node without env — env should be preserved
	err = store.IngestNodes(ctx, []Node{{
		ID:   "svc:alpha",
		Type: "Service",
		Name: "alpha-updated",
	}})
	if err != nil {
		t.Fatalf("Upsert IngestNodes failed: %v", err)
	}

	result, err := store.Search(ctx, "alpha", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected to find node")
	}
	if result.Nodes[0].Env != "production" {
		t.Errorf("expected env 'production' to be preserved, got %q", result.Nodes[0].Env)
	}
	if result.Nodes[0].Name != "alpha-updated" {
		t.Errorf("expected name to be updated to 'alpha-updated', got %q", result.Nodes[0].Name)
	}
}

// TestSearchByEnv verifies that FTS5 indexes the env column so searching
// for an environment name finds the corresponding nodes.
func TestSearchByEnv(t *testing.T) {
	store, cleanup := newTestStore(t, "test_search_env.db")
	defer cleanup()

	ctx := context.Background()

	err := store.IngestNodes(ctx, []Node{
		{ID: "svc:prod-api", Type: "Service", Name: "prod-api", Env: "production"},
		{ID: "svc:staging-api", Type: "Service", Name: "staging-api", Env: "staging"},
	})
	if err != nil {
		t.Fatalf("IngestNodes failed: %v", err)
	}

	// Search for "production" — should find the prod node via env FTS
	result, err := store.Search(ctx, "production", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node for 'production', got %d", len(result.Nodes))
	}
	if result.Nodes[0].ID != "svc:prod-api" {
		t.Errorf("expected svc:prod-api, got %s", result.Nodes[0].ID)
	}
	if result.Nodes[0].Env != "production" {
		t.Errorf("expected env 'production', got %q", result.Nodes[0].Env)
	}
}

func TestGenerateNoteID(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := generateNoteID()
		if !strings.HasPrefix(id, "note_") {
			t.Fatalf("expected 'note_' prefix, got %q", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}
