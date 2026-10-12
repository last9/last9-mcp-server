package prompts

import (
	"strings"
	"testing"
)

func TestBuildEnhancedDescriptionInjectsLabels(t *testing.T) {
	base := "Available attributes: {{labels}}."

	got := buildEnhancedDescription(base, []string{"service.name", "deployment.environment", "http.status_code"})

	want := "Available attributes: service.name, deployment.environment, http.status_code."
	if got != want {
		t.Fatalf("buildEnhancedDescription() = %q, want %q", got, want)
	}
}

func TestBuildEnhancedDescriptionReturnsDescriptionWithoutPlaceholderUnchanged(t *testing.T) {
	tests := []string{
		"Use discovery tools before filtering on structured attributes.",
		"Available attributes: {{labels.",
	}

	for _, base := range tests {
		t.Run(base, func(t *testing.T) {
			got := buildEnhancedDescription(base, []string{"service.name"})

			if got != base {
				t.Fatalf("buildEnhancedDescription() = %q, want unchanged %q", got, base)
			}
		})
	}
}

func TestBuildEnhancedDescriptionUsesFallbackForMissingLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
	}{
		{name: "nil labels", labels: nil},
		{name: "empty labels", labels: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEnhancedDescription("Available attributes: {{labels}}.", tt.labels)

			if strings.Contains(got, "{{labels}}") {
				t.Fatalf("buildEnhancedDescription() left placeholder unresolved: %q", got)
			}
			if !strings.Contains(got, "No attributes discovered") {
				t.Fatalf("buildEnhancedDescription() = %q, want fallback text", got)
			}
		})
	}
}
