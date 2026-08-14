package deeplink

import (
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

func TestAPMCatalogEnv(t *testing.T) {
	cases := map[string]string{
		"":             "",
		".*":           "",
		"prod":         "prod",
		"^prod$":       "prod",
		"prod|staging": "",
		"^prod.*$":     "",
		"  ^staging$ ": "staging",
	}
	for in, want := range cases {
		if got := APMCatalogEnv(in); got != want {
			t.Errorf("APMCatalogEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildAPMServiceLink_EnvSanitation(t *testing.T) {
	b := NewBuilder("acme", "c1")
	withFilter := b.BuildAPMServiceLink(1_000, 2_000, "api", "^prod$", "")
	if !strings.Contains(withFilter, "deployment_environment") || !strings.Contains(withFilter, "prod") {
		t.Fatalf("anchored env should set filter: %s", withFilter)
	}
	if strings.Contains(withFilter, "%5E") || strings.Contains(withFilter, "^prod$") {
		t.Fatalf("anchors must be stripped before filter: %s", withFilter)
	}

	for _, env := range []string{".*", "prod|staging", ""} {
		link := b.BuildAPMServiceLink(1_000, 2_000, "api", env, "")
		if strings.Contains(link, "deployment_environment") {
			t.Fatalf("env %q must not set catalog filter: %s", env, link)
		}
	}

	plain := b.BuildAPMServiceLink(1_000, 2_000, "api", "prod", "")
	if !strings.Contains(plain, "deployment_environment") {
		t.Fatalf("plain exact env from sibling tools should set filter: %s", plain)
	}
}
