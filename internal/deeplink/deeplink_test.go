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
