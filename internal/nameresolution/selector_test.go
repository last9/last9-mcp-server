package nameresolution

import "testing"

func TestMetricSelector(t *testing.T) {
	for _, tt := range []struct{ input, name, scope, corrected string }{
		{`checkuot_requests`, "checkuot_requests", `{__name__!=""}`, `checkout_requests`},
		{` checkuot_requests {env="prod",note="checkuot_requests"} `, "checkuot_requests", `{env="prod",note="checkuot_requests"}`, ` checkout_requests {env="prod",note="checkuot_requests"} `},
		{`{env="prod",__name__="checkuot_requests",job=~"web.*"}`, "checkuot_requests", `{env="prod",job=~"web.*"}`, `{env="prod",__name__="checkout_requests",job=~"web.*"}`},
		{`checkuot_requests{job!="api",note="brace } and \\\""}`, "checkuot_requests", `{job!="api",note="brace } and \\\""}`, `checkout_requests{job!="api",note="brace } and \\\""}`},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := ParseMetricSelector(tt.input)
			if !ok || got.Name != tt.name || got.Scope() != tt.scope || got.WithName("checkout_requests") != tt.corrected {
				t.Fatalf("selector=%+v ok=%v", got, ok)
			}
		})
	}
	for _, input := range []string{`sum(checkuot_requests)`, `rate(checkuot_requests[5m])`, `WITH (x = foo) x`, `foo or bar`, `{__name__=~"checkuot.*"}`, `{__name__!="foo"}`, `{job="foo"}`, `foo{__name__="bar"}`, `foo{env="x",env="y"}`, `foo{env=oops}`, `foo{env="x"} trailing`, `"checkuot_requests"`, `{__name__="foo",__name__="bar"}`} {
		if _, ok := ParseMetricSelector(input); ok {
			t.Errorf("unsafe selector accepted: %s", input)
		}
	}
}

func TestMetricSelectorQuotesCanonicalNames(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`requests`, `{__name__="request.count"}`},
		{`requests{env="prod"}`, `{__name__="request.count",env="prod"}`},
	} {
		p, ok := ParseMetricSelector(tt.input)
		if !ok {
			t.Fatal("invalid fixture")
		}
		if got := p.WithName("request.count"); got != tt.want {
			t.Errorf("query=%s; want %s", got, tt.want)
		}
	}
}
