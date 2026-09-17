package grafana

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const sampleFolders = `[
  {"id": 1, "uid": "fld1", "title": "Payments", "url": "/dashboards/f/payments"}
]`

func TestListFoldersHandler(t *testing.T) {
	var gotPath string
	srv := newRecordingServer("/api/folders", sampleFolders, new(string), &gotPath)
	defer srv.Close()

	result, _, err := NewListFoldersHandler(srv.Client(), testGrafanaConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, ListFoldersArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text

	if !strings.Contains(text, `"uid": "fld1"`) || !strings.Contains(text, "Payments") {
		t.Fatalf("folders result = %s", text)
	}
	if gotPath != "/api/folders" {
		t.Fatalf("folders request path = %q", gotPath)
	}

	var folders []Folder
	if err := json.Unmarshal([]byte(text), &folders); err != nil {
		t.Fatalf("folders result not valid JSON: %v", err)
	}
	if len(folders) != 1 || folders[0].UID != "fld1" || folders[0].Name != "Payments" {
		t.Fatalf("folders = %+v", folders)
	}
}
