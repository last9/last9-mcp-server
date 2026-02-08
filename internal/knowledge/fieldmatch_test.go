package knowledge

import (
	"testing"
)

func TestTokenizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"ServiceName", []string{"service", "name"}},
		{"service_name", []string{"service", "name"}},
		{"net-peer-name", []string{"net", "peer", "name"}},
		{"HTTPEndpoint", []string{"http", "endpoint"}},
		{"db_system", []string{"db", "system"}},
		{"DataStoreInstance", []string{"data", "store", "instance"}},
		{"Pod", []string{"pod"}},
		{"pod", []string{"pod"}},
		{"RPCSystem", []string{"rpc", "system"}},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := TokenizeName(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("TokenizeName(%q) = %v, want %v", tt.input, got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("TokenizeName(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestTokenSimilarity(t *testing.T) {
	tests := []struct {
		a, b     string
		minScore float64
		maxScore float64
	}{
		// Identical after tokenization
		{"service_name", "ServiceName", 1.0, 1.0},
		{"HTTPEndpoint", "http_endpoint", 1.0, 1.0},

		// Completely disjoint
		{"Pod", "DataStoreInstance", 0.0, 0.0},
		{"service", "database", 0.0, 0.0},

		// Partial overlap: "service" shares with "service_name"
		{"service", "service_name", 0.4, 0.6},

		// Same single token
		{"pod", "Pod", 1.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			score := TokenSimilarity(tt.a, tt.b)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("TokenSimilarity(%q, %q) = %f, want [%f, %f]",
					tt.a, tt.b, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestTokenSimilarity_EmptyInputs(t *testing.T) {
	if s := TokenSimilarity("", ""); s != 1.0 {
		t.Errorf("both empty should be 1.0, got %f", s)
	}
	if s := TokenSimilarity("service", ""); s != 0.0 {
		t.Errorf("one empty should be 0.0, got %f", s)
	}
	if s := TokenSimilarity("", "service"); s != 0.0 {
		t.Errorf("one empty should be 0.0, got %f", s)
	}
}

func TestMakeNodeID(t *testing.T) {
	tests := []struct {
		nodeType string
		parts    []string
		expected string
	}{
		{"Service", []string{"frontend"}, "service:frontend"},
		{"DataStoreInstance", []string{"mysql", "db-host"}, "datastoreinstance:mysql:db-host"},
		{"Pod", []string{}, "pod"},
		{"Service", []string{""}, "service"},
		{"Service", []string{" frontend "}, "service:frontend"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := MakeNodeID(tt.nodeType, tt.parts...)
			if got != tt.expected {
				t.Errorf("MakeNodeID(%q, %v) = %q, want %q", tt.nodeType, tt.parts, got, tt.expected)
			}
		})
	}
}

func TestResolveNodeType_Alias(t *testing.T) {
	schemaNodes := []string{"Service", "DataStoreInstance", "HTTPEndpoint", "Pod"}

	tests := []struct {
		field    string
		expected string
	}{
		{"service_name", "Service"},
		{"db_system", "DataStoreInstance"},
		{"endpoint", "HTTPEndpoint"},
		{"pod", "Pod"},
		{"unknown_field", ""},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := ResolveNodeType(tt.field, schemaNodes)
			if got != tt.expected {
				t.Errorf("ResolveNodeType(%q) = %q, want %q", tt.field, got, tt.expected)
			}
		})
	}
}

func TestResolveNodeType_TokenSimilarityFallback(t *testing.T) {
	schemaNodes := []string{"Service", "DataStoreInstance", "HTTPEndpoint"}

	// "http_endpoint" is not in FieldAliases but should match HTTPEndpoint via similarity
	got := ResolveNodeType("http_endpoint", schemaNodes)
	if got != "HTTPEndpoint" {
		t.Errorf("expected HTTPEndpoint via similarity, got %q", got)
	}
}
