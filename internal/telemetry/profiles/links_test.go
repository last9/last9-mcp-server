package profiles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func profilesLinkServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write(dataframeBody(map[string]any{
				"StackHash": "1",
				"Frames":    "leaf" + FrameDelimiter + "main",
				"samples":   float64(10),
			}))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func profilesReferenceQuery(t *testing.T, res *mcp.CallToolResult) url.Values {
	t.Helper()
	if res == nil || res.IsError {
		t.Fatalf("unexpected result: %+v", res)
	}
	raw, ok := res.Meta["reference_url"].(string)
	if !ok || raw == "" {
		t.Fatalf("missing reference_url in meta: %v", res.Meta)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse reference_url %q: %v", raw, err)
	}
	if u.Path != "/v2/organizations/acme/profiles" {
		t.Fatalf("path = %q", u.Path)
	}
	return u.Query()
}

func TestProfilesHandlers_ProfilesLink(t *testing.T) {
	start := time.Date(2026, time.February, 9, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	srv := profilesLinkServer(t, http.StatusOK)
	cfg := testCfg(srv.URL)
	cfg.OrgSlug = "acme"
	cfg.ClusterID = "last9-cluster-id"
	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	startISO, endISO := start.Format(time.RFC3339), end.Format(time.RFC3339)

	cases := []struct {
		name string
		run  func() (*mcp.CallToolResult, any, error)
		want map[string]string
	}{
		{"flamegraph", func() (*mcp.CallToolResult, any, error) {
			return NewGetFlamegraphHandler(srv.Client(), cfg)(ctx, req, GetFlamegraphArgs{
				Service: "api", Env: "prod", Cluster: "k8s-east", Namespace: "payments", Runtime: "go",
				ProfileType: "alloc", StartTimeISO: startISO, EndTimeISO: endISO,
			})
		}, map[string]string{
			"from": "1770631200", "to": "1770633000", "service": "api", "env": "prod",
			"cluster": "k8s-east", "namespace": "payments", "runtime": "go",
			"profileType": "alloc", "profileTab": "flamegraph",
		}},
		{"top_functions", func() (*mcp.CallToolResult, any, error) {
			return NewGetTopFunctionsHandler(srv.Client(), cfg)(ctx, req, GetTopFunctionsArgs{
				Service: "api", StartTimeISO: startISO, EndTimeISO: endISO,
			})
		}, map[string]string{"from": "1770631200", "to": "1770633000", "service": "api", "profileTab": "top-functions"}},
		{"summary", func() (*mcp.CallToolResult, any, error) {
			return NewGetProfileSummaryHandler(srv.Client(), cfg)(ctx, req, GetProfileSummaryArgs{
				Service: "api", ProfileType: "wall", StartTimeISO: startISO, EndTimeISO: endISO,
			})
		}, map[string]string{"from": "1770631200", "to": "1770633000", "service": "api", "profileType": "wall"}},
		{"services", func() (*mcp.CallToolResult, any, error) {
			return NewGetProfileServicesHandler(srv.Client(), cfg)(ctx, req, GetProfileServicesArgs{
				Env: "prod", StartTimeISO: startISO, EndTimeISO: endISO,
			})
		}, map[string]string{"from": "1770631200", "to": "1770633000", "env": "prod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := tc.run()
			if err != nil {
				t.Fatal(err)
			}
			q := profilesReferenceQuery(t, res)
			for k, v := range tc.want {
				if got := q.Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
			// Exact param set: no cluster ID leak, no defaulted profileType or tab.
			if len(q) != len(tc.want) {
				t.Errorf("params = %v, want exactly %v", q, tc.want)
			}
		})
	}
}

func TestProfilesHandlers_NoLinkOnError(t *testing.T) {
	srv := profilesLinkServer(t, http.StatusInternalServerError)
	cfg := testCfg(srv.URL)
	cfg.OrgSlug = "acme"

	res, _, err := NewGetFlamegraphHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetFlamegraphArgs{Service: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error result, got %+v", res)
	}
	if _, ok := res.Meta["reference_url"]; ok {
		t.Fatalf("error result must not carry a link: %v", res.Meta)
	}
}
