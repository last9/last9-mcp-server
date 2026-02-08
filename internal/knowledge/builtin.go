package knowledge

import (
	"context"
	"embed"
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed schemas/*.yaml
var builtinSchemaFS embed.FS

// LoadBuiltinSchemas reads and parses all embedded YAML schema files.
func LoadBuiltinSchemas() ([]Schema, error) {
	entries, err := builtinSchemaFS.ReadDir("schemas")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded schemas directory: %w", err)
	}

	var schemas []Schema
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		data, err := builtinSchemaFS.ReadFile(filepath.Join("schemas", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read schema file %s: %w", entry.Name(), err)
		}

		var schema Schema
		if err := yaml.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("failed to parse schema file %s: %w", entry.Name(), err)
		}

		// Ensure builtin flag is set regardless of YAML content
		schema.Builtin = true

		schemas = append(schemas, schema)
	}

	return schemas, nil
}

// RegisterBuiltinSchemas loads all embedded schemas and registers them in the store.
// Uses RegisterBuiltinSchema which preserves user-assigned services across restarts.
func RegisterBuiltinSchemas(ctx context.Context, store Store) error {
	schemas, err := LoadBuiltinSchemas()
	if err != nil {
		return fmt.Errorf("failed to load builtin schemas: %w", err)
	}

	for _, schema := range schemas {
		if err := store.RegisterBuiltinSchema(ctx, schema); err != nil {
			return fmt.Errorf("failed to register builtin schema '%s': %w", schema.Name, err)
		}
	}

	return nil
}
