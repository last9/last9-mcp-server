package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServedAlertDescriptionsDocumentResultLinks(t *testing.T) {
	for _, spec := range []string{"all", "alerts"} {
		t.Run(spec, func(t *testing.T) {
			allowed, err := toolsets.Parse(spec)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := dumpTools(&buf, toolsets.Filter{Tools: allowed}); err != nil {
				t.Fatal(err)
			}
			var out struct {
				Tools []*mcp.Tool `json:"tools"`
			}
			if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			descriptions := make(map[string]string)
			for _, tool := range out.Tools {
				descriptions[tool.Name] = tool.Description
			}
			for _, name := range []string{"create_alert", "update_alert", "patch_alert", "create_alert_from_chart", "create_kpi"} {
				t.Run(name, func(t *testing.T) {
					want := []string{"_meta.reference_url", "_meta.links", "Open alert", "Open group", "saved rule", "only when the API supplies a group name"}
					if name == "create_kpi" {
						want = []string{"_meta.reference_url", "_meta.links", "Open alert group", "groups index"}
					}
					for _, phrase := range want {
						if !strings.Contains(descriptions[name], phrase) {
							t.Errorf("%s description missing %q", name, phrase)
						}
					}
				})
			}
		})
	}
}
