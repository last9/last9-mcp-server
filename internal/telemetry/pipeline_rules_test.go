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
