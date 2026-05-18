package deeplink

import "testing"

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
