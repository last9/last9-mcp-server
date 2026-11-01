package nameresolution

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResolveCatalog(t *testing.T) {
	tests := []struct {
		name, requested string
		catalog         Catalog
		status          string
		canonical       string
		candidates      []string
	}{
		{"exact wins", "checkout", Catalog{[]string{"checkout", "check-out"}, true}, "exact", "checkout", nil},
		{"safe typo", "checkuot", Catalog{[]string{"checkout"}, true}, "closest", "checkout", nil},
		{"ambiguous", "checkuot", Catalog{[]string{"checkout", "checkouts"}, true}, "ambiguous", "", []string{"checkout", "checkouts"}},
		{"missing", "billing", Catalog{[]string{"checkout"}, true}, "missing", "", nil},
		{"empty complete", "billing", Catalog{[]string{}, true}, "missing", "", nil},
		{"incomplete refuses typo", "checkuot", Catalog{[]string{"checkout"}, false}, "incomplete", "", []string{"checkout"}},
		{"incomplete candidates are unranked", "billing", Catalog{[]string{"checkout"}, false}, "incomplete", "", []string{"checkout"}},
		{"incomplete is not missing", "billing", Catalog{nil, false}, "incomplete", "", nil},
		{"exact in incomplete", "checkout", Catalog{[]string{"checkout"}, false}, "exact", "checkout", nil},
		{"duplicate records", "checkuot", Catalog{[]string{"checkout", "checkout"}, true}, "closest", "checkout", nil},
		{"wrong region", "checkout-eu", Catalog{[]string{"checkout-us"}, true}, "missing", "", nil},
		{"wrong instance", "checkout-2", Catalog{[]string{"checkout-1"}, true}, "missing", "", nil},
		{"unsafe single candidate", "checkout", Catalog{[]string{"checkout-prod"}, true}, "ambiguous", "", []string{"checkout-prod"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve("service", tt.requested, tt.catalog)
			if got.Status != tt.status || got.Canonical != tt.canonical || !reflect.DeepEqual(got.Candidates, tt.candidates) {
				t.Fatalf("resolution = %+v; want %s, %q, %v", got, tt.status, tt.canonical, tt.candidates)
			}
			if got.Canonical != "" {
				if got.Result() != nil {
					t.Fatal("resolved name returned a refusal")
				}
			} else {
				result := got.Result()
				if result == nil || result.IsError {
					t.Fatalf("expected non-error refusal: %+v", result)
				}
				var payload map[string]any
				if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &payload); err != nil {
					t.Fatal(err)
				}
				if payload["status"] != tt.status || payload["requested"] != tt.requested {
					t.Fatalf("payload = %v", payload)
				}
			}
			note := got.Note()
			if tt.status == "closest" {
				for _, want := range []string{"closest match", tt.requested, tt.canonical} {
					if !strings.Contains(note, want) {
						t.Fatalf("note %q missing %q", note, want)
					}
				}
			} else if note != "" {
				t.Fatalf("unexpected note: %s", note)
			}
		})
	}
}

func TestResolveCandidatesBoundedAndStable(t *testing.T) {
	names := []string{"web-7", "web-3", "web-1", "web-6", "web-2", "web-5", "web-4"}
	original := append([]string(nil), names...)
	got := Resolve("service", "web", Catalog{names, true})
	if len(got.Candidates) != 5 || got.Omitted != 2 || got.Candidates[0] != "web-1" {
		t.Fatalf("resolution = %+v", got)
	}
	if !reflect.DeepEqual(original, names) {
		t.Fatal("mutated catalog")
	}
}
