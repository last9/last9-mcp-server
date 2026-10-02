package alerting

import (
	"strings"
	"testing"
)

// TestSearchTermOmitsTeamTierLabels guards the bug introduced in commit
// 5d9757f, which added Team, Tier, and Labels to alertGroupEntity /
// alertGroupEntityMetadata and rendered all three in every
// get_alert_config rule row via formatAlertConfigResponse, but did not
// extend matchesAlertConfigSearchTerm (the function backing the
// search_term filter) to search them.
//
// Before the fix, a search term that appeared only in Tier, Team, or a
// label key/value silently returned "Found 0 alert rules:" even though the
// same rule row displayed the matching Team/Tier/Labels data. These cases
// use the shared sampleAlertConfigRules / sampleAlertGroupEntities fixtures
// and the executeGetAlertConfig harness so they exercise the full handler
// path (fetch -> enrich -> filter -> format).
func TestSearchTermOmitsTeamTierLabels(t *testing.T) {
	newState := func() alertConfigTestServerState {
		return alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupEntities(),
			alertRulesStatus:   200,
			entityLookupStatus: 200,
		}
	}

	t.Run("search term unique to tier matches", func(t *testing.T) {
		// "p1" appears only in entity-1's Tier ("p1") and nowhere else in
		// entity-1's or rule-1's searchable fields.
		state := newState()
		text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{
			SearchTerm: "p1",
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		assertAlertConfigResultIDs(t, text, []string{"rule-1"})
		if !strings.Contains(text, "Tier: p1") {
			t.Fatalf("expected Tier display in matched rule row, got:\n%s", text)
		}
	})

	t.Run("search term unique to label key matches", func(t *testing.T) {
		// "domain" appears only as a label key on entity-1; it is absent
		// from rule names, entity names/types/data sources, tags, team,
		// and tier. This is the realistic user-facing scenario: searching
		// for rules by a label key that appears across many groups.
		state := newState()
		text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{
			SearchTerm: "domain",
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		assertAlertConfigResultIDs(t, text, []string{"rule-1"})
		if !strings.Contains(text, "Labels: domain=checkout, env=prod") {
			t.Fatalf("expected Labels display in matched rule row, got:\n%s", text)
		}
	})

	t.Run("search term unique to label value matches", func(t *testing.T) {
		// "staging" appears as a label value (env=staging) on entity-2.
		// It also appears in entity-2's Tags (["payments", "staging"]),
		// so this case matches via the pre-existing tag path as well —
		// included to confirm label-value search does not regress when a
		// value overlaps with another searchable field.
		state := newState()
		text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{
			SearchTerm: "staging",
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		assertAlertConfigResultIDs(t, text, []string{"rule-2"})
		if !strings.Contains(text, "Labels: env=staging") {
			t.Fatalf("expected Labels display in matched rule row, got:\n%s", text)
		}
	})

	t.Run("search term matching existing fields still matches", func(t *testing.T) {
		// "checkout" appears in entity-1's Name ("Checkout Alerts"),
		// Tags ("checkout"), Team ("checkout"), and label value
		// (domain=checkout). It matches via the pre-existing Name/Tags
		// path; this case is a control ensuring the harness stays
		// correct and the new search branches do not break the happy
		// path.
		state := newState()
		text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{
			SearchTerm: "checkout",
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		assertAlertConfigResultIDs(t, text, []string{"rule-1"})
		if !strings.Contains(text, "Team: checkout") ||
			!strings.Contains(text, "Tier: p1") ||
			!strings.Contains(text, "Labels: domain=checkout, env=prod") {
			t.Fatalf("expected Team/Tier/Labels display in matched rule row, got:\n%s", text)
		}
	})

	t.Run("search term with no matches returns empty", func(t *testing.T) {
		state := newState()
		text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{
			SearchTerm: "this-substring-does-not-exist-anywhere",
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		assertAlertConfigResultIDs(t, text, []string{})
	})

	t.Run("search term combines with typed filter via AND", func(t *testing.T) {
		// Severity "threat" restricts to rule-2; "domain" (label key on
		// entity-1 only) would match rule-1. AND semantics must yield no
		// rules, confirming search_term still composes with typed
		// filters after the fix.
		state := newState()
		text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{
			Severity:   "threat",
			SearchTerm: "domain",
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		assertAlertConfigResultIDs(t, text, []string{})
	})
}

// TestMatchesAlertConfigSearchTerm exercises the pure predicate directly
// for the newly-searched fields and the edge cases the handler-level test
// above cannot express: nil/empty labels must not panic, and entity-only
// terms (incl. the new team/tier/label fields) must not match when the
// entity is absent. Pre-existing fields (rule name, entity name/type/data
// source, tags) are covered end-to-end by TestGetAlertConfigHandler_* and
// are not duplicated here.
func TestMatchesAlertConfigSearchTerm(t *testing.T) {
	// Entity whose searchable fields are deliberately distinct so a
	// substring can be attributed to exactly one field.
	isolatedEntity := alertGroupEntity{
		Name:           "Alpha Group",
		Type:           "grafana-dashboard",
		Tier:           "p0",
		DataSourceName: "Grafana Source",
		Metadata: alertGroupEntityMetadata{
			Tags:   []string{"alpha-tag"},
			Team:   "platform-squad",
			Labels: map[string]string{"cost-center": "12345", "Env": "Prod"},
		},
	}

	tests := []struct {
		name        string
		rule        AlertRule
		entity      alertGroupEntity
		entityFound bool
		searchTerm  string
		want        bool
	}{
		// New searchable fields introduced by the fix.
		{
			name:        "team match",
			entity:      isolatedEntity,
			entityFound: true,
			searchTerm:  "platform",
			want:        true,
		},
		{
			name:        "tier match",
			entity:      isolatedEntity,
			entityFound: true,
			searchTerm:  "p0",
			want:        true,
		},
		{
			name:        "label key match",
			entity:      isolatedEntity,
			entityFound: true,
			searchTerm:  "cost-center",
			want:        true,
		},
		{
			name:        "label value match",
			entity:      isolatedEntity,
			entityFound: true,
			searchTerm:  "12345",
			want:        true,
		},
		{
			name:        "label match is case-insensitive (key and value)",
			entity:      isolatedEntity,
			entityFound: true,
			searchTerm:  "PROD",
			want:        true, // matches label value "Prod" case-insensitively
		},

		// entityFound gating: entity-only terms (incl. new fields) must
		// not produce a false positive when the entity is absent.
		{
			name:        "team-only term does not match when entity not found",
			rule:        AlertRule{RuleName: "Unrelated"},
			entity:      isolatedEntity,
			entityFound: false,
			searchTerm:  "platform-squad",
			want:        false,
		},
		{
			name:        "tier-only term does not match when entity not found",
			rule:        AlertRule{RuleName: "Unrelated"},
			entity:      isolatedEntity,
			entityFound: false,
			searchTerm:  "p0",
			want:        false,
		},
		{
			name:        "label-only term does not match when entity not found",
			rule:        AlertRule{RuleName: "Unrelated"},
			entity:      isolatedEntity,
			entityFound: false,
			searchTerm:  "cost-center",
			want:        false,
		},

		// Nil / empty labels must not panic and must not match.
		{
			name:        "nil labels does not panic and does not match label term",
			entity:      alertGroupEntity{Name: "Alpha Group", Metadata: alertGroupEntityMetadata{Team: "sre"}},
			entityFound: true,
			searchTerm:  "cost-center",
			want:        false,
		},
		{
			name:        "empty labels map does not panic",
			entity:      alertGroupEntity{Name: "Alpha Group", Metadata: alertGroupEntityMetadata{Labels: map[string]string{}}},
			entityFound: true,
			searchTerm:  "cost-center",
			want:        false,
		},

		// Negative: term absent from all fields does not match.
		{
			name:        "no field matches",
			rule:        AlertRule{RuleName: "High latency"},
			entity:      isolatedEntity,
			entityFound: true,
			searchTerm:  "zzz-not-present",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAlertConfigSearchTerm(tt.rule, tt.entity, tt.entityFound, tt.searchTerm)
			if got != tt.want {
				t.Fatalf("matchesAlertConfigSearchTerm(%q) = %v, want %v", tt.searchTerm, got, tt.want)
			}
		})
	}
}
