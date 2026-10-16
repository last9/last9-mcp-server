package apm

import (
	"strings"
	"testing"
)

func TestLiteralEnvPattern(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		want        string
		wantLiteral string
		wantErr     bool
	}{
		{name: "empty means all", in: "", want: ".*", wantLiteral: ""},
		{name: "dot-star means all", in: ".*", want: ".*", wantLiteral: ""},
		{name: "anchored dot-star means all", in: "^.*$", want: ".*", wantLiteral: ""},
		{name: "plain", in: "production", want: "production", wantLiteral: "production"},
		{name: "dotted name is escaped", in: "prod.eu", want: `prod\.eu`, wantLiteral: "prod.eu"},
		{name: "hyphenated name", in: "prod-1", want: "prod-1", wantLiteral: "prod-1"},
		{name: "anchors stripped", in: "^production$", want: "production", wantLiteral: "production"},
		{name: "non-capturing group anchors stripped", in: "^(?:production)$", want: "production", wantLiteral: "production"},
		{name: "capturing group anchors stripped", in: "^(production)$", want: "production", wantLiteral: "production"},
		{name: "anchored dotted name", in: "^prod.eu$", want: `prod\.eu`, wantLiteral: "prod.eu"},
		{name: "alternation rejected", in: "prod|staging", wantErr: true},
		{name: "wildcard suffix rejected", in: "prod.*", wantErr: true},
		{name: "anchored alternation rejected", in: "^(?:prod|staging)$", wantErr: true},
		{name: "wildcard-plus rejected", in: ".+x", wantErr: true},
		{name: "prefix wildcard rejected", in: ".*prod", wantErr: true},
		{name: "plus alone is literal", in: "prod+", want: `prod\+`, wantLiteral: "prod+"},
		{name: "question is literal", in: "prod?", want: `prod\?`, wantLiteral: "prod?"},
		{name: "brackets are literal", in: "prod[blue]", want: `prod\[blue\]`, wantLiteral: "prod[blue]"},
		{name: "dotted brackets are literal", in: "prod.v1", want: `prod\.v1`, wantLiteral: "prod.v1"},
		{name: "braces are literal", in: "prod{1}", want: `prod\{1\}`, wantLiteral: "prod{1}"},
		{name: "parentheses elsewhere are literal", in: "prod(blue)", want: `prod\(blue\)`, wantLiteral: "prod(blue)"},
		{name: "backslash is literal", in: `prod\d`, want: `prod\\d`, wantLiteral: `prod\d`},
		{name: "half anchor is literal", in: "^production", want: `\^production`, wantLiteral: "^production"},
		{name: "empty anchored rejected", in: "^$", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotLiteral, err := literalEnvPattern(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("literalEnvPattern(%q) = %q, want error", tt.in, got)
				}
				want := `env "` + tt.in + `" looks like a regular expression; this tool matches one environment name exactly (for example "production"). Use get_service_environments to list names.`
				if !strings.Contains(err.Error(), "looks like a regular expression") || err.Error() != want {
					t.Errorf("unexpected error text: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("literalEnvPattern(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("literalEnvPattern(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if gotLiteral != tt.wantLiteral {
				t.Errorf("literalEnvPattern(%q) literal = %q, want %q", tt.in, gotLiteral, tt.wantLiteral)
			}
		})
	}
}
