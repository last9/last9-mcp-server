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
		{"equal distance is ambiguous", "stagini", []string{"staging", "stagink"}, Match{Kind: Ambiguous, Candidates: []string{"staging", "stagink"}}},
		{"lower total distance wins", "paymnt-servce", []string{"payment-service", "paymnt-services"}, Match{Kind: Closest, Name: "payment-service"}},
		{"separators dropped", "checkoutapi", names, Match{Kind: Closest, Name: "checkout_api"}},
		{"short token swapped", "prod-uk", names, Match{Kind: None}},
		{"region word swapped", "orders - east (system)", []string{"orders - west (system)"}, Match{Kind: None}},
		{"region swapped in a long name", "logs-cluster-apsouth-production (system)", []string{"logs-cluster-apsoutheast-production (system)"}, Match{Kind: None}},
		{"instance number differs", "ingest-gateway-2 - eu-central (system)", []string{"ingest-gateway-1 - eu-central (system)", "ingest-gateway - eu-central (system)"}, Match{Kind: None}},
		{"version differs", "metrics-v1.3-testing", []string{"metrics-v1.2-testing"}, Match{Kind: None}},
		{"typo beside a number", "metrics-v1.2-tseting", []string{"metrics-v1.2-testing"}, Match{Kind: Closest, Name: "metrics-v1.2-testing"}},
		{"extra token", "staging-2", names, Match{Kind: None}},
		{"substring never splits a number", "web-1", []string{"web-12", "web-13"}, Match{Kind: None}},
		{"substring may end at a number", "web", []string{"web-12", "db"}, Match{Kind: Closest, Name: "web-12"}},
		{"short input never fuzzy matches", "st", names, Match{Kind: None}},
		{"too far", "billing", names, Match{Kind: None}},
		{"one edit over budget", "stapinx", names, Match{Kind: None}},
		{"short word one edit over budget", "east", []string{"west", "staging"}, Match{Kind: None}},
		{"length gap at budget", "stagingxx", names, Match{Kind: Closest, Name: "staging"}},
		{"length gap counted in runes", "stagingéé", names, Match{Kind: Closest, Name: "staging"}},
		{"length gap over budget", "stagingxxx", names, Match{Kind: None}},
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
