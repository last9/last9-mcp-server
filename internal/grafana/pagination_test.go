package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func paginatedSearchServer(t *testing.T, mode string, all []SearchHit) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/folders/f" {
			fmt.Fprint(w, `{"id":7,"uid":"f","title":"Synthetic"}`)
			return
		}
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		if mode == "folder" && r.URL.Query().Get("folderIds") != "7" {
			t.Error("lost folder filter")
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 5000 {
			limit = 1000
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		start := min((page-1)*limit, len(all))
		end := min(start+limit, len(all))
		_ = json.NewEncoder(w).Encode(all[start:end])
	}))
}

// TestReviewDashboardPagination is the regression for the finding that search
// and folder listing silently stopped at the default first page (1000 rows).
// Pages are walked until a short page or the row cap; the folder filter must
// survive on every page. 1001 rows must come back complete, and crossing
// maxSearchRows must be flagged truncated rather than quietly trimmed.
func TestReviewDashboardPagination(t *testing.T) {
	for _, mode := range []string{"search", "folder"} {
		for _, count := range []int{1, 1000, 1001} {
			t.Run(fmt.Sprintf("%s/%d", mode, count), func(t *testing.T) {
				all := make([]SearchHit, count)
				for i := range all {
					all[i] = SearchHit{ID: i + 1, UID: fmt.Sprintf("synthetic-%d", i), Title: "Synthetic service", Type: "dash-db"}
				}
				srv := paginatedSearchServer(t, mode, all)
				defer srv.Close()

				var result *mcp.CallToolResult
				var err error
				if mode == "folder" {
					result, _, err = NewListFolderDashboardsHandler(srv.Client(), testGrafanaConfig(srv.URL))(
						context.Background(), nil, ListFolderDashboardsArgs{FolderUID: "f"})
				} else {
					result, _, err = NewSearchDashboardsHandler(srv.Client(), testGrafanaConfig(srv.URL))(
						context.Background(), nil, SearchDashboardsArgs{Query: "Synthetic"})
				}
				if err != nil {
					t.Fatal(err)
				}
				var res SearchResults
				if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &res); err != nil {
					t.Fatal(err)
				}
				if len(res.Dashboards) != count {
					t.Fatalf("silently omitted dashboards: got %d, want %d", len(res.Dashboards), count)
				}
				if res.Truncated {
					t.Fatalf("unexpected truncation for %d rows", count)
				}
			})
		}
	}
}

func TestReviewDashboardPaginationTruncated(t *testing.T) {
	all := make([]SearchHit, maxSearchRows+1)
	for i := range all {
		all[i] = SearchHit{ID: i + 1, UID: fmt.Sprintf("synthetic-%d", i), Title: "Synthetic service", Type: "dash-db"}
	}
	srv := paginatedSearchServer(t, "search", all)
	defer srv.Close()

	res, _, err := NewSearchDashboardsHandler(srv.Client(), testGrafanaConfig(srv.URL))(
		context.Background(), nil, SearchDashboardsArgs{Query: "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	var out SearchResults
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Dashboards) != maxSearchRows {
		t.Fatalf("truncated rows = %d, want %d", len(out.Dashboards), maxSearchRows)
	}
	if !out.Truncated {
		t.Fatalf("expected truncated flag at %d rows", maxSearchRows+1)
	}
}
