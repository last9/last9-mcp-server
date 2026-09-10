package telemetry

import (
	"math"
	"strings"
	"testing"
)

func TestValidateQuantileFunctionExact(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []interface{}
		want string
	}{
		{"valid trace duration", []interface{}{0.99, "Duration"}, ""},
		{"valid log descriptor", []interface{}{0.5, "attributes['duration_ms']"}, ""},
		{"non finite", []interface{}{math.Inf(1), "Duration"}, "$quantile_exact[0]"},
		{"out of range", []interface{}{-0.1, "Duration"}, "$quantile_exact[0]"},
		{"wrong count", []interface{}{0.9}, "$quantile_exact"},
		{"unsafe field type", []interface{}{0.9, 1}, "$quantile_exact[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateQuantileFunction(map[string]interface{}{"$quantile_exact": tc.args}, "pipeline[0].function")
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Path, tc.want)) {
				t.Fatalf("error = %#v, want %q", err, tc.want)
			}
		})
	}
}
