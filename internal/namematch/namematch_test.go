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
		{"unique substring is only a candidate", "payment", names, Match{Kind: Ambiguous, Candidates: []string{"payment-service"}}},
		{"shared substring is ambiguous", "prod", names, Match{Kind: Ambiguous, Candidates: []string{"prod-eu", "prod-us"}}},
		{"equal distance is ambiguous", "stagini", []string{"staging", "stagink"}, Match{Kind: Ambiguous, Candidates: []string{"staging", "stagink"}}},
		{"two edits are only a candidate", "paymnt-servce", []string{"payment-service", "paymnt-services"}, Match{Kind: Ambiguous, Candidates: []string{"payment-service"}}},
		{"a farther name in budget makes it ambiguous", "checkuot", []string{"checkout", "checkouts"}, Match{Kind: Ambiguous, Candidates: []string{"checkout", "checkouts"}}},
		{"edit budget is absolute in a long word", "observabilitiplatfornmetricz", []string{"observabilityplatformmetrics"}, Match{Kind: None}},
		{"region word swapped inside a word", "clusteruswestproductionlogs", []string{"clusteruseastproductionlogs"}, Match{Kind: None}},
		{"region code swapped inside a word", "orders-afsouth", []string{"orders-apsouth"}, Match{Kind: None}},
		{"region code also in an unchanged prefix", "canaryca", []string{"canarysa"}, Match{Kind: None}},
		{"region code also inside another word", "customermesouth", []string{"customermxsouth"}, Match{Kind: None}},
		{"us swapped for uk inside a word", "telemetryus", []string{"telemetryuk"}, Match{Kind: None}},
		{"uk swapped for us inside a word", "telemetryuk", []string{"telemetryus"}, Match{Kind: None}},
		{"listed code swapped for an unlisted one is only offered", "telemetryus", []string{"telemetryua"}, Match{Kind: Ambiguous, Candidates: []string{"telemetryua"}}},
		{"unlisted code swapped for a listed one is only offered", "telemetryua", []string{"telemetryus"}, Match{Kind: Ambiguous, Candidates: []string{"telemetryus"}}},
		{"two unlisted codes swapped are only offered", "telemetryfr", []string{"telemetrytr"}, Match{Kind: Ambiguous, Candidates: []string{"telemetrytr"}}},
		{"two unlisted codes swapped back are only offered", "telemetrytr", []string{"telemetryfr"}, Match{Kind: Ambiguous, Candidates: []string{"telemetryfr"}}},
		{"one substituted letter is only offered", "ordars", []string{"orders"}, Match{Kind: Ambiguous, Candidates: []string{"orders"}}},
		{"unlisted codes transposed at a word end are only offered", "telemetryes", []string{"telemetryse"}, Match{Kind: Ambiguous, Candidates: []string{"telemetryse"}}},
		{"unlisted codes transposed back are only offered", "telemetryse", []string{"telemetryes"}, Match{Kind: Ambiguous, Candidates: []string{"telemetryes"}}},
		{"unlisted codes transposed at a word start are only offered", "estelemetry", []string{"setelemetry"}, Match{Kind: Ambiguous, Candidates: []string{"setelemetry"}}},
		{"listed code transposed at a word end is only offered", "telemetryau", []string{"telemetryua"}, Match{Kind: Ambiguous, Candidates: []string{"telemetryua"}}},
		{"typo beside a repeated region code", "canrayca", []string{"canaryca"}, Match{Kind: Closest, Name: "canaryca"}},
		{"transposition that spells a region code", "dmeo-api", []string{"demo-api"}, Match{Kind: Closest, Name: "demo-api"}},
		{"transposition that spells a region code mid-word", "seucrity", []string{"security"}, Match{Kind: Closest, Name: "security"}},
		{"env word swapped inside a word", "checkoutqalogs", []string{"checkoutuatlogs"}, Match{Kind: None}},
		{"dropped separators keep numbers apart", "db-1-2-3", []string{"db123"}, Match{Kind: None}},
		{"dropped separators keep versions apart", "metrics-v1.23", []string{"metrics-v12.3"}, Match{Kind: None}},
		{"dropped separator beside a number", "web1", []string{"web-1"}, Match{Kind: Closest, Name: "web-1"}},
		{"separators dropped", "checkoutapi", names, Match{Kind: Closest, Name: "checkout_api"}},
		{"short token swapped", "prod-uk", names, Match{Kind: None}},
		{"region word swapped", "orders - east (system)", []string{"orders - west (system)"}, Match{Kind: None}},
		{"region swapped in a long name", "logs-cluster-apsouth-production (system)", []string{"logs-cluster-apsoutheast-production (system)"}, Match{Kind: None}},
		{"instance number differs", "ingest-gateway-2 - eu-central (system)", []string{"ingest-gateway-1 - eu-central (system)", "ingest-gateway - eu-central (system)"}, Match{Kind: None}},
		{"version differs", "metrics-v1.3-testing", []string{"metrics-v1.2-testing"}, Match{Kind: None}},
		{"typo beside a number", "metrics-v1.2-tseting", []string{"metrics-v1.2-testing"}, Match{Kind: Closest, Name: "metrics-v1.2-testing"}},
		{"extra token", "staging-2", names, Match{Kind: None}},
		{"substring never splits a number", "web-1", []string{"web-12", "web-13"}, Match{Kind: None}},
		{"substring may end at a number", "web", []string{"web-12", "db"}, Match{Kind: Ambiguous, Candidates: []string{"web-12"}}},
		{"missing instance suffix is only a candidate", "ingest-eu", []string{"ingest-eu-2"}, Match{Kind: Ambiguous, Candidates: []string{"ingest-eu-2"}}},
		{"short input never fuzzy matches", "st", names, Match{Kind: None}},
		{"too far", "billing", names, Match{Kind: None}},
		{"one edit over budget", "stapinx", names, Match{Kind: None}},
		{"short word one edit over budget", "east", []string{"west", "staging"}, Match{Kind: None}},
		{"length gap at budget is only a candidate", "stagingxx", names, Match{Kind: Ambiguous, Candidates: []string{"staging"}}},
		{"length gap counted in runes", "stagingéé", names, Match{Kind: Ambiguous, Candidates: []string{"staging"}}},
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
	if !reflect.DeepEqual(got.Candidates, want) || got.Omitted != 2 {
		t.Errorf("Candidates = %v, Omitted = %d; want %v, 2", got.Candidates, got.Omitted, want)
	}
}
