package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContractsRejectsUnsafeOrAmbiguousDescriptors(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "contracts.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	for _, body := range []string{
		`[{"datasource":"prod","source":"logs","schema_version":1},{"datasource":"prod","source":"logs","schema_version":1}]`,
		`[{"datasource":"","source":"logs","schema_version":1}]`,
		`[{"datasource":"prod","source":"logs","schema_version":0}]`,
		`[{"datasource":"prod","source":"model","schema_version":1}]`,
		`[] []`,
	} {
		if _, err := LoadContracts(write(t, body)); err == nil {
			t.Fatalf("LoadContracts(%s) succeeded", body)
		}
	}
}

func TestContractsLookupUsesConfiguredBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contracts.json")
	if err := os.WriteFile(path, []byte(`[{"datasource":"prod","source":"logs","index":"physical_index: app","schema_version":1,"fields":{"ServiceName":"string"},"backend_limits":{"no_hidden_sampling":true,"max_rows":101}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	contracts, err := LoadContracts(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := contracts.Lookup("prod", "logs", "physical_index:app", 1); !ok {
		t.Fatal("configured descriptor was not found")
	}
	for _, bad := range [][4]any{{"other", "logs", "physical_index:app", 1}, {"prod", "traces", "", 1}, {"prod", "logs", "physical_index:other", 1}, {"prod", "logs", "physical_index:app", 2}} {
		if _, ok := contracts.Lookup(bad[0].(string), bad[1].(string), bad[2].(string), bad[3].(int)); ok {
			t.Fatalf("unexpected descriptor lookup hit: %#v", bad)
		}
	}
}
