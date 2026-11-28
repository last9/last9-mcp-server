package logs

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

func TestGetServiceLogs_MultipleBodyFiltersAnnotateRawRows(t *testing.T) {
	out := callGetServiceLogs(t, []summaryTestLine{
		{severity: "ERROR", message: `transaction TXN-0001 failed`},
		{severity: "WARN", message: `status TRANSACTION_EXPIRED`},
		{severity: "ERROR", message: `txn-0001 is expired`},
	}, GetServiceLogsArgs{
		BodyFilters:  []string{"TXN-0001", "EXPIRED"},
		ResponseMode: "raw",
	})

	logs := out["logs"].([]any)
	want := map[string][]any{
		`transaction TXN-0001 failed`: {"TXN-0001"},
		`status TRANSACTION_EXPIRED`:  {"EXPIRED"},
		`txn-0001 is expired`:         {"TXN-0001", "EXPIRED"},
	}
	for _, entry := range logs {
		row := entry.(map[string]any)
		got, expected := row["matched_terms"], want[row["message"].(string)]
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("message %q matched_terms = %v, want %v", row["message"], got, expected)
		}
	}
}

func TestGetServiceLogs_NormalizesBodyFiltersBeforeAnnotatingRows(t *testing.T) {
	out := callGetServiceLogs(t, []summaryTestLine{{message: "request timeout failed"}}, GetServiceLogsArgs{
		BodyFilters:  []string{" timeout ", "", "timeout", "failed"},
		ResponseMode: "raw",
	})

	row := out["logs"].([]any)[0].(map[string]any)
	want := []any{"timeout", "failed"}
	if got := row["matched_terms"]; !reflect.DeepEqual(got, want) {
		t.Errorf("matched_terms = %v, want %v", got, want)
	}
}

func TestGetServiceLogs_OneBodyFilterDoesNotAnnotateRows(t *testing.T) {
	out := callGetServiceLogs(t, []summaryTestLine{{message: "request timed out"}}, GetServiceLogsArgs{
		BodyFilters:  []string{"timeout"},
		ResponseMode: "raw",
	})
	if _, ok := out["logs"].([]any)[0].(map[string]any)["matched_terms"]; ok {
		t.Error("one body filter added matched_terms")
	}
}

func TestGetServiceLogs_MultipleBodyFiltersAddSummaryCounts(t *testing.T) {
	lines := make([]summaryTestLine, maxRawLinesInSummaryMode+1)
	for i := range lines {
		message := fmt.Sprintf("TXN-%04d accepted", i)
		if i%2 == 0 {
			message += " EXPIRED"
		}
		lines[i] = summaryTestLine{message: message}
	}
	out := callGetServiceLogs(t, lines, GetServiceLogsArgs{
		BodyFilters: []string{"txn-", "expired", "missing"},
	})

	summary := out["summary"].(map[string]any)
	want := map[string]any{"txn-": float64(len(lines)), "expired": float64(11), "missing": float64(0)}
	if got := summary["matched_term_counts"]; !reflect.DeepEqual(got, want) {
		t.Errorf("matched_term_counts = %v, want %v", got, want)
	}
}

func TestServiceLogsBodyFilterDocsExplainAnyAndAllTerms(t *testing.T) {
	if !strings.Contains(prompts.GetServiceLogsDescription, "`last9://reference/service_logs`") {
		t.Error("description missing service logs reference")
	}
	if !strings.Contains(prompts.GetServiceLogsDescription, "`body_filters` match ANY term") {
		t.Error("description missing body_filters ANY semantics")
	}
	for _, want := range []string{
		"ANY term",
		"ALL terms",
		"returned lines only",
		"`get_logs`",
		"`$and`",
		"`$containsWords` uses word matching",
		"`body_filters` use substring matching",
	} {
		if !strings.Contains(prompts.ServiceLogsReference, want) {
			t.Errorf("reference missing %q", want)
		}
	}
}
