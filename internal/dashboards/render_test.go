package dashboards

import (
	_ "embed"
	"encoding/json"
	"testing"
)

//go:embed testdata/k8s-rightsizing/expected.api.json
var goldenK8sExpected []byte

func TestRenderPlaceholders_K8sRightsizing_Golden(t *testing.T) {
	tmplBytes, err := templateFS.ReadFile("templates/k8s-rightsizing/dashboard.api.json.tmpl")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	knobs := map[string]string{
		"DASHBOARD_NAME": "Golden — K8s Rightsizing",
		"NAMESPACES":     "prod|staging",
		"CLUSTERS":       ".*",
		"WINDOW":         "360",
	}

	got, err := RenderPlaceholders(string(tmplBytes), knobs)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var gotJSON, wantJSON any
	if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
		t.Fatalf("rendered output not valid JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal(goldenK8sExpected, &wantJSON); err != nil {
		t.Fatalf("golden not valid JSON: %v", err)
	}

	gotNorm, _ := json.Marshal(gotJSON)
	wantNorm, _ := json.Marshal(wantJSON)
	if string(gotNorm) != string(wantNorm) {
		t.Fatalf("render mismatch\ngot:  %s\nwant: %s", gotNorm, wantNorm)
	}
}

func TestRenderPlaceholders_UnresolvedError(t *testing.T) {
	_, err := RenderPlaceholders(`{"name":"{{.MISSING}}"}`, map[string]string{})
	if err == nil {
		t.Fatal("expected error for unresolved placeholder")
	}
}

func TestRenderPlaceholders_InvalidJSONError(t *testing.T) {
	_, err := RenderPlaceholders(`{not json}`, map[string]string{})
	if err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
}
