package knowledge

import (
	"context"
	"os"
	"testing"
)

func TestLoadBuiltinSchemas(t *testing.T) {
	schemas, err := LoadBuiltinSchemas()
	if err != nil {
		t.Fatalf("LoadBuiltinSchemas failed: %v", err)
	}

	if len(schemas) != 4 {
		t.Fatalf("expected 4 builtin schemas, got %d", len(schemas))
	}

	expectedNames := map[string]bool{
		"http_k8s_datastore":  false,
		"ingest_gateway":      false,
		"kafka_consumer_jobs": false,
		"http_vm_datastore":   false,
	}

	for _, s := range schemas {
		if !s.Builtin {
			t.Errorf("schema %s should have Builtin=true", s.Name)
		}
		if s.Description == "" {
			t.Errorf("schema %s should have a description", s.Name)
		}
		if len(s.Blueprint.Nodes) == 0 {
			t.Errorf("schema %s should have nodes", s.Name)
		}
		if len(s.Blueprint.Edges) == 0 {
			t.Errorf("schema %s should have edges", s.Name)
		}
		if _, ok := expectedNames[s.Name]; !ok {
			t.Errorf("unexpected schema name: %s", s.Name)
		}
		expectedNames[s.Name] = true
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected schema %s not found", name)
		}
	}
}

func TestRegisterBuiltinSchemas(t *testing.T) {
	tmpDB := "test_builtin_register.db"
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

	schemas, err := store.ListSchemas(ctx)
	if err != nil {
		t.Fatalf("ListSchemas failed: %v", err)
	}

	if len(schemas) != 4 {
		t.Fatalf("expected 4 schemas after registration, got %d", len(schemas))
	}

	for _, s := range schemas {
		if !s.Builtin {
			t.Errorf("schema %s should be marked as builtin", s.Name)
		}
	}
}

func TestBuiltinSchemaPreservesServicesOnReregister(t *testing.T) {
	tmpDB := "test_builtin_preserve.db"
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

	// Add a service to a builtin schema
	services := []string{"payment-api", "auth-service"}
	if err := store.UpdateSchemaServices(ctx, "http_k8s_datastore", services); err != nil {
		t.Fatalf("UpdateSchemaServices failed: %v", err)
	}

	// Re-register builtin schemas (simulates restart)
	if err := RegisterBuiltinSchemas(ctx, store); err != nil {
		t.Fatalf("Re-RegisterBuiltinSchemas failed: %v", err)
	}

	// Verify services are preserved
	schema, err := store.GetSchema(ctx, "http_k8s_datastore")
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}

	if len(schema.Services) != 2 {
		t.Fatalf("expected 2 services after re-register, got %d: %v", len(schema.Services), schema.Services)
	}

	serviceSet := make(map[string]bool)
	for _, s := range schema.Services {
		serviceSet[s] = true
	}
	if !serviceSet["payment-api"] || !serviceSet["auth-service"] {
		t.Errorf("expected services [payment-api, auth-service], got %v", schema.Services)
	}
}
