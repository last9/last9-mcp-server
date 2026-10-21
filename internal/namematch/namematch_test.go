package namematch

import (
	"reflect"
	"testing"
)

func TestResolve(t *testing.T) {
	names := []string{"prod-us", "prod-eu", "staging", "payment-service", "checkout_api"}

	tests := []struct {
		name  string
		input string
		names []string
		want  Match
	}{
		{"exact", "staging", names, Match{Kind: Exact, Name: "staging"}},
		{"case folded", "STAGING", names, Match{Kind: Closest, Name: "staging"}},
		{"separators folded", "checkout-api", names, Match{Kind: Closest, Name: "checkout_api"}},
		{"spaces trimmed", " staging ", names, Match{Kind: Closest, Name: "staging"}},
		{"one typo", "stagign", names, Match{Kind: Closest, Name: "staging"}},
		{"one missing letter", "payment-servce", names, Match{Kind: Closest, Name: "payment-service"}},
		{"unique substring", "payment", names, Match{Kind: Closest, Name: "payment-service"}},
		{"shared substring is ambiguous", "prod", names, Match{Kind: Ambiguous, Candidates: []string{"prod-eu", "prod-us"}}},
		{"equal distance is ambiguous", "prod-uu", names, Match{Kind: Ambiguous, Candidates: []string{"prod-eu", "prod-us"}}},
		{"short input never fuzzy matches", "st", names, Match{Kind: None}},
		{"too far", "billing", names, Match{Kind: None}},
		{"blank input", "  ", names, Match{Kind: None}},
		{"no names", "staging", nil, Match{Kind: None}},
		{"case variants of one name are ambiguous", "api", []string{"API", "Api"}, Match{Kind: Ambiguous, Candidates: []string{"API", "Api"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.input, tt.names); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Resolve(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveCapsCandidates(t *testing.T) {
	names := []string{"web-1", "web-2", "web-3", "web-4", "web-5", "web-6", "web-7"}
	got := Resolve("web", names)
	if got.Kind != Ambiguous {
		t.Fatalf("Kind = %v, want Ambiguous", got.Kind)
	}
	want := []string{"web-1", "web-2", "web-3", "web-4", "web-5"}
	if !reflect.DeepEqual(got.Candidates, want) {
		t.Errorf("Candidates = %v, want %v", got.Candidates, want)
	}
}
