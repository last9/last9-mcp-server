package telemetry

import (
	"strings"
	"testing"
)

func TestCheckNumericComparisonShowsJSONNumber(t *testing.T) {
	err := CheckNumericComparison("$gt", "SeverityText", "high", "filter.$gt[1]")
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), `{"$gt":["attributes['http.status_code']",500]}`) {
		t.Fatalf("error example is not a JSON number: %v", err)
	}
}

func TestIsMapFieldRef(t *testing.T) {
	for _, field := range []string{
		`attributes['label']`,
		`resources['deployment.zone']`,
		`attributes['label[0]']`,
		`resources['deployment[zone]']`,
	} {
		if !IsMapFieldRef(field) {
			t.Errorf("IsMapFieldRef(%q) = false, want true", field)
		}
	}
	for _, field := range []string{
		`attributes["label"]`,
		`attributes['label\name']`,
		`attributes['label'] trailing`,
		`events['name']`,
	} {
		if IsMapFieldRef(field) {
			t.Errorf("IsMapFieldRef(%q) = true, want false", field)
		}
	}
}
