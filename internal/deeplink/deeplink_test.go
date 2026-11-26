package deeplink

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestBuildDashboardLink(t *testing.T) {
	b := NewBuilder("acme", "cluster-1")
	got := b.BuildDashboardLink("uuid-1")
	want := "/v2/organizations/acme/dashboards/uuid-1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildDashboardsIndexLink(t *testing.T) {
	b := NewBuilder("acme", "cluster-1")
	got := b.BuildDashboardsIndexLink()
	want := "/v2/organizations/acme/dashboards"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildExceptionLink(t *testing.T) {
	b := NewBuilder("acme", "cluster-1")

	narrowed := b.BuildExceptionLink(1_700_000_000_000, 1_700_003_600_000, ExceptionRow{ID: "1b589f62-web-GET /a&b", Env: "prod"})
	want := "/v2/organizations/acme/exceptions?cluster=cluster-1" +
		"&exception_id=1b589f62-web-GET+%2Fa%26b" +
		"&filter=%5B%22deployment_environment%3D%5C%22prod%5C%22%22%5D" +
		"&from=1700000000&to=1700003600"
	if narrowed != want {
		t.Fatalf("narrowed link:\n got %s\nwant %s", narrowed, want)
	}
	parsed, err := url.Parse(narrowed)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("filter"); got != `["deployment_environment=\"prod\""]` {
		t.Fatalf("filter = %s, want the dashboard's getFilterString shape", got)
	}

	notSet := b.BuildExceptionLink(1_700_000_000_000, 1_700_003_600_000, ExceptionRow{ID: "1b589f62-web-View"})
	parsed, err = url.Parse(notSet)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("filter"); got != `["deployment_environment=\"\""]` {
		t.Fatalf("a row without env must select Not Set, overriding the reader's saved env; filter = %s", got)
	}
	if got := parsed.Query().Get("exception_id"); got != "1b589f62-web-View" {
		t.Fatalf("exception_id = %q", got)
	}
}

func TestBuildExceptionLinkRejectsUnsafeEnvironment(t *testing.T) {
	b := NewBuilder("acme", "cluster-1")
	for _, tc := range []struct {
		name string
		env  string
		want bool
	}{
		{name: "normal", env: "prod", want: true},
		{name: "quoted", env: `prod" or "staging`, want: false},
		{name: "control character", env: "prod\nstaging", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := b.BuildExceptionLink(1_700_000_000_000, 1_700_003_600_000, ExceptionRow{ID: "1b589f62-web-View", Env: tc.env})
			if (got != "") != tc.want {
				t.Fatalf("BuildExceptionLink() = %q, want link present = %t", got, tc.want)
			}
		})
	}
}

func TestBuildRemappingLink(t *testing.T) {
	b := NewBuilder("acme", "cluster-1")
	got := b.BuildRemappingLink()
	want := "/v2/organizations/acme/control-plane/cluster-1/remapping"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAPMCatalogEnvExact(t *testing.T) {
	cases := map[string]string{
		"":             "",
		".*":           "",
		"prod":         "prod",
		"k8s.prod":     "k8s.prod",
		"prod.us-east": "prod.us-east",
		"team(a)":      "team(a)",
		"prod-us-east": "prod-us-east",
		"  staging  ":  "staging",
	}
	for in, want := range cases {
		if got := APMCatalogEnvExact(in); got != want {
			t.Errorf("APMCatalogEnvExact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPMCatalogEnvFromRegex(t *testing.T) {
	cases := map[string]string{
		"":             "",
		".*":           "",
		"prod":         "",
		"^prod$":       "prod",
		"prod|staging": "",
		"^prod.*$":     "",
		"k8s.prod":     "",
		"  ^staging$ ": "staging",
	}
	for in, want := range cases {
		if got := APMCatalogEnvFromRegex(in); got != want {
			t.Errorf("APMCatalogEnvFromRegex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildAPMServiceLink_EnvSanitation(t *testing.T) {
	b := NewBuilder("acme", "c1")

	// Exact callers: dotted / parenthesized names keep their filter.
	for _, env := range []string{"prod", "k8s.prod", "prod.us-east", "team(a)"} {
		link := b.BuildAPMServiceLink(1_000, 2_000, "api", env, "")
		decoded, _ := url.QueryUnescape(link)
		if !strings.Contains(decoded, "deployment_environment") || !strings.Contains(decoded, env) {
			t.Fatalf("exact env %q should set catalog filter: %s", env, decoded)
		}
	}

	for _, env := range []string{".*", ""} {
		link := b.BuildAPMServiceLink(1_000, 2_000, "api", env, "")
		if strings.Contains(link, "deployment_environment") {
			t.Fatalf("env %q must not set catalog filter: %s", env, link)
		}
	}

	// Regex callers pre-sanitize; only ^name$ becomes a literal passed in.
	literal := APMCatalogEnvFromRegex("^prod$")
	withFilter := b.BuildAPMServiceLink(1_000, 2_000, "api", literal, "")
	if !strings.Contains(withFilter, "deployment_environment") || !strings.Contains(withFilter, "prod") {
		t.Fatalf("anchored regex should become a literal catalog filter: %s", withFilter)
	}
	if strings.Contains(withFilter, "^prod$") {
		t.Fatalf("anchors must be stripped before filter: %s", withFilter)
	}

	for _, env := range []string{".*", "prod|staging", "prod", "^prod.*$"} {
		link := b.BuildAPMServiceLink(1_000, 2_000, "api", APMCatalogEnvFromRegex(env), "")
		if strings.Contains(link, "deployment_environment") {
			t.Fatalf("regex env %q must not set catalog filter: %s", env, link)
		}
	}
}

func TestBuildMetricsLink(t *testing.T) {
	b := NewBuilder("acme", "cluster-1")
	link := b.BuildMetricsLink(1_700_000_000_000, 1_700_000_900_000, `sum(rate(http_requests_total{job="api"}[5m]))`)

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse %q: %v", link, err)
	}
	if u.Path != "/v2/organizations/acme/metrics" {
		t.Fatalf("path = %q, want /v2/organizations/acme/metrics", u.Path)
	}
	q := u.Query()
	want := map[string]string{
		"from":    "1700000000",
		"to":      "1700000900",
		"cluster": "cluster-1",
		"mode":    "code",
		"query":   `sum(rate(http_requests_total{job="api"}[5m]))`,
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestBuildMetricsLink_NoClusterNoQuery(t *testing.T) {
	link := NewBuilder("acme", "").BuildMetricsLink(1_000, 2_000, "")
	want := "/v2/organizations/acme/metrics?from=1&to=2"
	if link != want {
		t.Fatalf("got %q want %q", link, want)
	}
}

func TestBuildAPMServiceLink_Catalog(t *testing.T) {
	link := NewBuilder("acme", "c1").BuildAPMServiceLink(1_000, 2_000, "", "", "")
	want := "/v2/organizations/acme/service-catalog?cluster=c1&from=1&live=true&to=2"
	if link != want {
		t.Fatalf("got %q want %q", link, want)
	}
}

func TestBuildProfilesLink(t *testing.T) {
	link := NewBuilder("acme", "last9-cluster-id").BuildProfilesLink(1_700_000_000_000, 1_700_003_600_000, ProfilesLinkParams{
		Service:     "checkout",
		Env:         "prod",
		Cluster:     "k8s-east",
		Namespace:   "payments",
		Runtime:     "go",
		ProfileType: "alloc",
		ProfileTab:  "top-functions",
	})
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse %q: %v", link, err)
	}
	if u.Path != "/v2/organizations/acme/profiles" {
		t.Fatalf("path = %q, want /v2/organizations/acme/profiles", u.Path)
	}
	q := u.Query()
	want := map[string]string{
		"from":        "1700000000",
		"to":          "1700003600",
		"service":     "checkout",
		"env":         "prod",
		"cluster":     "k8s-east",
		"namespace":   "payments",
		"runtime":     "go",
		"profileType": "alloc",
		"profileTab":  "top-functions",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if len(q) != len(want) {
		t.Errorf("unexpected params: %v", q)
	}
}

func TestBuildProfilesLink_OmitsEmptyAndNeverUsesClusterID(t *testing.T) {
	link := NewBuilder("acme", "last9-cluster-id").BuildProfilesLink(1_000, 2_000, ProfilesLinkParams{})
	want := "/v2/organizations/acme/profiles?from=1&to=2"
	if link != want {
		t.Fatalf("got %q want %q", link, want)
	}
}

func TestBuildGrafanaLink(t *testing.T) {
	b := NewBuilder("acme", "c1")
	tests := []struct {
		path    string
		wantURL string
	}{
		{"/dashboards", "/api/gp/dashboards"},
		{"/dashboards/f/" + url.PathEscape("folder 1"), "/api/gp/dashboards/f/folder%201"},
		{"/d/" + url.PathEscape("abc/123"), "/api/gp/d/abc%2F123"},
	}
	for _, tt := range tests {
		link := b.BuildGrafanaLink(tt.path)
		u, err := url.Parse(link)
		if err != nil {
			t.Fatalf("parse %q: %v", link, err)
		}
		if u.Path != "/v2/organizations/acme/grafana" {
			t.Errorf("path = %q", u.Path)
		}
		if got := u.Query().Get("url"); got != tt.wantURL {
			t.Errorf("url = %q, want %q", got, tt.wantURL)
		}
		if len(u.Query()) != 1 {
			t.Errorf("unexpected params: %v", u.Query())
		}
	}
	if got, want := b.BuildGrafanaLink("/dashboards"), "/v2/organizations/acme/grafana?url=%2Fapi%2Fgp%2Fdashboards"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestBuildExceptionsLink_Filters(t *testing.T) {
	b := NewBuilder("acme", "c1")
	tests := []struct {
		name        string
		service     string
		env         string
		wantFilters []string
		wantNoLink  bool
	}{
		{name: "service and env", service: "last9-api", env: "alpha", wantFilters: []string{`service_name="last9-api"`, `deployment_environment="alpha"`}},
		{name: "dotted service stays literal", service: "checkout.v2", wantFilters: []string{`service_name="checkout.v2"`}},
		{name: "service only", service: "api", wantFilters: []string{`service_name="api"`}},
		{name: "env only", env: "prod", wantFilters: []string{`deployment_environment="prod"`}},
		{name: "regex character stays literal", service: "api|web", wantFilters: []string{`service_name="api|web"`}},
		{name: "regex characters in env stay literal", env: "prod.*", wantFilters: []string{`deployment_environment="prod.*"`}},
		{name: "quoted service suppresses link", service: `api"web`, env: "prod", wantNoLink: true},
		{name: "control character suppresses link", service: "api\nweb", wantNoLink: true},
		{name: "quoted env suppresses link", service: "api", env: `prod"`, wantNoLink: true},
		{name: "no filters", wantFilters: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := b.BuildExceptionsLink(ExceptionsLinkOptions{
				FromMs:      1_000,
				ToMs:        2_000,
				ServiceName: tt.service,
				Env:         tt.env,
			})
			if tt.wantNoLink {
				if link != "" {
					t.Fatalf("want no link, got %q", link)
				}
				return
			}
			u, err := url.Parse(link)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			q := u.Query()
			if u.Path != "/v2/organizations/acme/exceptions" || q.Get("from") != "1" || q.Get("to") != "2" || q.Get("cluster") != "c1" {
				t.Fatalf("unexpected base link: %s", u.String())
			}
			raw := q.Get("filter")
			if tt.wantFilters == nil {
				if raw != "" {
					t.Fatalf("want no filter, got %s", raw)
				}
				return
			}
			var got []string
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("filter %q is not a JSON array: %v", raw, err)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFilters, ",") {
				t.Fatalf("got %v want %v", got, tt.wantFilters)
			}
		})
	}
}
