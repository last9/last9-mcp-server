package deeplink

import (
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
