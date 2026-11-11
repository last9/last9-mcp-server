package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"go.opentelemetry.io/otel/attribute"
)

func TestGetLogPatternsPhaseTiming(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "search failure"}[failed], func(t *testing.T) {
			stub := &logSearchStub{}
			search := stub.handler(map[int64][]string{patternWindowStart: {"worker ready"}}, nil)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(5 * time.Millisecond)
				if failed {
					http.Error(w, "unavailable", http.StatusInternalServerError)
					return
				}
				search(w, r)
			}))
			defer srv.Close()
			ctx, finish := utils.RecordedSpanContext(t)
			result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(ctx, nil, GetLogPatternsArgs{
				LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO},
			})
			if err != nil {
				t.Fatalf("handler returned Go error: %v", err)
			}
			if result == nil || result.IsError != failed {
				t.Fatalf("result = %#v, want IsError %v", result, failed)
			}
			attrs := finish()
			for _, phase := range []string{"sample", "mine", "deep_link"} {
				key := attribute.Key("last9.phase." + phase + ".duration_ms")
				value, ok := attrs[key]
				if failed && phase != "sample" {
					if ok {
						t.Errorf("unexecuted %s was timed", phase)
					}
					continue
				}
				if !ok || value.Type() != attribute.FLOAT64 || value.AsFloat64() < 0 {
					t.Errorf("missing duration for %s: %v", phase, value)
				}
				if phase == "sample" && value.AsFloat64() < 5 {
					t.Errorf("sample duration %v excludes search delay", value)
				}
				if got := attrs[attribute.Key("last9.phase."+phase+".failed")].AsBool(); got != failed {
					t.Errorf("%s failed = %v", phase, got)
				}
			}
		})
	}
}

func TestGetLogPatternsInvalidArgsHaveNoPhaseTiming(t *testing.T) {
	ctx, finish := utils.RecordedSpanContext(t)
	_, _, err := NewGetLogPatternsHandler(http.DefaultClient, apiModeCfg("http://unused"))(ctx, nil, GetLogPatternsArgs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if attrs := finish(); len(attrs) != 0 {
		t.Fatalf("invalid call recorded work: %v", attrs)
	}
	_, _, err = NewGetLogPatternsHandler(http.DefaultClient, apiModeCfg("http://unused"))(context.Background(), nil, GetLogPatternsArgs{})
	if err == nil {
		t.Fatal("expected validation error without tracing")
	}
}
