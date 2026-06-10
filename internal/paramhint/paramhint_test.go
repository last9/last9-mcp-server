package paramhint

import (
	"strings"
	"testing"
)

func TestOffendingKeys(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want []string
	}{
		{
			"single key",
			`calling "tools/call": invalid params: validating "arguments": validating root: unexpected additional properties ["match"]`,
			[]string{"match"},
		},
		{
			"multiple keys",
			`validating root: unexpected additional properties ["foo" "bar"]`,
			[]string{"foo", "bar"},
		},
		{
			"unrelated error",
			"connection refused",
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := offendingKeys(tc.err)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestSuggest(t *testing.T) {
	valid := []string{"service_name", "env", "lookback_minutes"}
	cases := []struct {
		key  string
		want string
	}{
		{"servicename", "service_name"},
		{"service_nme", "service_name"},
		{"frobnicate", ""},
		{"ENV", "env"},
	}
	for _, tc := range cases {
		if got := suggest(tc.key, valid); got != tc.want {
			t.Errorf("suggest(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestHint(t *testing.T) {
	r := NewRegistry()
	r.Register("get_service_summary", []string{"service_name", "service", "env", "lookback_minutes", "start_time_iso", "end_time_iso"})

	t.Run("near match gets one suggestion plus param list", func(t *testing.T) {
		h := r.Hint("get_service_summary", `unexpected additional properties ["servicename"]`)
		if !strings.Contains(h, `did you mean "service_name"`) {
			t.Fatalf("missing suggestion: %s", h)
		}
		if !strings.Contains(h, "Valid parameters for get_service_summary") {
			t.Fatalf("missing param list: %s", h)
		}
	})

	t.Run("unregistered tool yields no hint", func(t *testing.T) {
		if h := r.Hint("nope", `unexpected additional properties ["x"]`); h != "" {
			t.Fatalf("expected empty hint, got: %s", h)
		}
	})

	t.Run("non-validation error yields no hint", func(t *testing.T) {
		if h := r.Hint("get_service_summary", "boom"); h != "" {
			t.Fatalf("expected empty hint, got: %s", h)
		}
	})
}
