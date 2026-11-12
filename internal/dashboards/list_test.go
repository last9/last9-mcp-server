package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListDashboardsHandler_ReturnsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointDashboards {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "uuid-1", "name": "Test"}})
	}))
	defer srv.Close()

	handler := NewListDashboardsHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "uuid-1") {
		t.Fatalf("body %s", text)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok {
		t.Fatalf("expected reference_url in meta, got %v", result.Meta)
	}
	if refURL != "/v2/organizations/test-org/dashboards" {
		t.Fatalf("reference_url %q", refURL)
	}
}

type listedPage struct {
	Dashboards []map[string]any `json:"dashboards"`
	Total      int              `json:"total"`
	NextCursor string           `json:"next_cursor"`
}

// overviewFixture mirrors the API's overview row with every field populated.
func overviewFixture(n, nameLen int) []map[string]any {
	rows := make([]map[string]any, n)
	for i := range rows {
		name := fmt.Sprintf("Team %03d service overview ", i)
		name += strings.Repeat("x", max(0, nameLen-len(name)))
		rows[i] = map[string]any{
			"id":         fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
			"name":       name,
			"created_by": fmt.Sprintf("someone.with.a.long.address-%03d@example.com", i),
			"updated_at": strconv.Itoa(1793683205 - i),
			"tags":       []string{"production", "kubernetes", "golden-signals", "team-platform", "slo"},
			"readonly":   i%7 == 0,
		}
	}
	return rows
}

func dashboardListServer(t *testing.T, rows any) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callListDashboards(t *testing.T, srv *httptest.Server, args ListDashboardsArgs) (string, error) {
	t.Helper()
	handler := NewListDashboardsHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", err
	}
	return result.Content[0].(*mcp.TextContent).Text, nil
}

func listPage(t *testing.T, srv *httptest.Server, args ListDashboardsArgs) (listedPage, int) {
	t.Helper()
	text, err := callListDashboards(t, srv, args)
	if err != nil {
		t.Fatal(err)
	}
	var page listedPage
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}
	return page, len(text)
}

func listAllPages(t *testing.T, srv *httptest.Server, args ListDashboardsArgs) []string {
	t.Helper()
	var ids []string
	for range 1000 {
		page, size := listPage(t, srv, args)
		if size >= listDashboardsResponseBound {
			t.Fatalf("page of %d dashboards is %d bytes, want < %d", len(page.Dashboards), size, listDashboardsResponseBound)
		}
		for _, d := range page.Dashboards {
			ids = append(ids, d["id"].(string))
		}
		if page.NextCursor == "" {
			return ids
		}
		args.Cursor = page.NextCursor
	}
	t.Fatal("cursor paging never ended")
	return nil
}

func wantIDs(rows []map[string]any) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r["id"].(string)
	}
	return ids
}

func TestListDashboardsDefaultPageStaysUnderResponseBound(t *testing.T) {
	rows := overviewFixture(500, 60)
	srv := dashboardListServer(t, rows)

	page, size := listPage(t, srv, ListDashboardsArgs{})
	if size >= listDashboardsResponseBound {
		t.Fatalf("default response is %d bytes, want < %d", size, listDashboardsResponseBound)
	}
	if len(page.Dashboards) != defaultListDashboardsLimit || page.Total != 500 || page.NextCursor == "" {
		t.Fatalf("got %d dashboards, total %d, next_cursor %q", len(page.Dashboards), page.Total, page.NextCursor)
	}
	for key := range page.Dashboards[0] {
		if key != "id" && key != "name" && key != "updated_at" && key != "created_by" && key != "locked" {
			t.Errorf("listed field %q, want only summary and lock metadata", key)
		}
	}
}

func TestListDashboardsHandler_ServesLockedPerRow(t *testing.T) {
	rows := []map[string]any{
		{"id": "a", "name": "A", "readonly": true, "created_by": "user-7"},
		{"id": "b", "name": "B", "readonly": false},
		{"id": "c", "name": "C"},
	}
	page, _ := listPage(t, dashboardListServer(t, rows), ListDashboardsArgs{})
	if len(page.Dashboards) != 3 || page.Dashboards[0]["locked"] != true || page.Dashboards[1]["locked"] != false || page.Dashboards[0]["created_by"] != "user-7" {
		t.Fatalf("rows %#v", page.Dashboards)
	}
	if _, ok := page.Dashboards[2]["locked"]; ok {
		t.Fatalf("row without readonly gained locked: %#v", page.Dashboards[2])
	}
}

func TestListDashboardsCursorPagingReturnsEveryDashboardOnce(t *testing.T) {
	rows := overviewFixture(500, 60)
	got := listAllPages(t, dashboardListServer(t, rows), ListDashboardsArgs{})
	if strings.Join(got, ",") != strings.Join(wantIDs(rows), ",") {
		t.Fatalf("paged %d ids, want all %d in API order", len(got), len(rows))
	}
}

func TestListDashboardsMaxLimitStillFitsBoundWithLongNames(t *testing.T) {
	rows := overviewFixture(450, 255)
	srv := dashboardListServer(t, rows)
	limit := maxListDashboardsLimit

	page, _ := listPage(t, srv, ListDashboardsArgs{Limit: &limit})
	if len(page.Dashboards) == 0 || len(page.Dashboards) >= limit || page.NextCursor == "" {
		t.Fatalf("got %d dashboards with next_cursor %q, want a byte-trimmed page", len(page.Dashboards), page.NextCursor)
	}
	got := listAllPages(t, srv, ListDashboardsArgs{Limit: &limit})
	if strings.Join(got, ",") != strings.Join(wantIDs(rows), ",") {
		t.Fatalf("paged %d ids, want all %d in API order", len(got), len(rows))
	}
}

func TestListDashboardsLimit(t *testing.T) {
	srv := dashboardListServer(t, overviewFixture(3, 20))
	for _, tc := range []struct {
		limit     int
		want      int
		hasCursor bool
	}{
		{limit: 1, want: 1, hasCursor: true},
		{limit: 2, want: 2, hasCursor: true},
		{limit: 3, want: 3},
		{limit: 4, want: 3},
		{limit: maxListDashboardsLimit, want: 3},
	} {
		limit := tc.limit
		page, _ := listPage(t, srv, ListDashboardsArgs{Limit: &limit})
		if len(page.Dashboards) != tc.want || (page.NextCursor != "") != tc.hasCursor {
			t.Errorf("limit %d: got %d dashboards, next_cursor %q", tc.limit, len(page.Dashboards), page.NextCursor)
		}
	}
	for _, bad := range []int{0, -1, maxListDashboardsLimit + 1} {
		limit := bad
		if _, err := callListDashboards(t, srv, ListDashboardsArgs{Limit: &limit}); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Errorf("limit %d: err %v, want a limit error", bad, err)
		}
	}
}

func TestListDashboardsNameFilter(t *testing.T) {
	rows := []map[string]any{
		{"id": "a", "name": "Checkout API", "updated_at": "t1"},
		{"id": "b", "name": "Payments", "updated_at": "t2"},
		{"id": "c", "name": "checkout worker", "updated_at": "t3"},
		{"id": "d", "name": "CHECKOUT db", "updated_at": "t4"},
	}
	srv := dashboardListServer(t, rows)
	limit := 2

	first, _ := listPage(t, srv, ListDashboardsArgs{Name: "  Checkout ", Limit: &limit})
	if first.Total != 3 || len(first.Dashboards) != 2 || first.NextCursor == "" {
		t.Fatalf("first page %+v", first)
	}
	rest, _ := listPage(t, srv, ListDashboardsArgs{Name: "checkout", Limit: &limit, Cursor: first.NextCursor})
	if rest.Total != 3 || len(rest.Dashboards) != 1 || rest.Dashboards[0]["id"] != "d" || rest.NextCursor != "" {
		t.Fatalf("second page %+v", rest)
	}

	none, _ := listPage(t, srv, ListDashboardsArgs{Name: "inventory"})
	if none.Total != 0 || none.Dashboards == nil || len(none.Dashboards) != 0 || none.NextCursor != "" {
		t.Fatalf("no-match page %+v", none)
	}
}

func TestListDashboardsRejectsForeignCursors(t *testing.T) {
	srv := dashboardListServer(t, overviewFixture(5, 20))
	limit := 2
	page, _ := listPage(t, srv, ListDashboardsArgs{Limit: &limit})

	for name, tc := range map[string]struct {
		args ListDashboardsArgs
		want string
	}{
		"garbage":       {ListDashboardsArgs{Cursor: "not-a-cursor"}, "malformed"},
		"negative skip": {ListDashboardsArgs{Cursor: encodeListCursor(-1, "")}, "malformed"},
		"other filter":  {ListDashboardsArgs{Cursor: page.NextCursor, Name: "team"}, "different name"},
	} {
		if _, err := callListDashboards(t, srv, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want a cursor error containing %q", name, err, tc.want)
		}
	}
}

func TestListDashboardsCursorPastShrunkListReturnsEmptyPage(t *testing.T) {
	srv := dashboardListServer(t, overviewFixture(2, 20))
	page, _ := listPage(t, srv, ListDashboardsArgs{Cursor: encodeListCursor(5, "")})
	if page.Total != 2 || len(page.Dashboards) != 0 || page.NextCursor != "" {
		t.Fatalf("page %+v", page)
	}
}

func TestListDashboardsBodies(t *testing.T) {
	for _, body := range []string{"[]", "null"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		page, _ := listPage(t, srv, ListDashboardsArgs{})
		srv.Close()
		if page.Total != 0 || page.Dashboards == nil || page.NextCursor != "" {
			t.Errorf("body %s: page %+v", body, page)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"unexpected"}`))
	}))
	defer srv.Close()
	if _, err := callListDashboards(t, srv, ListDashboardsArgs{}); err == nil {
		t.Fatal("want an error for a non-array body")
	}
}
