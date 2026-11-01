package apm

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMetricProbeBounds(t *testing.T) {
	const end = int64(1700000300)
	for _, n := range []int{3, 4, 11, 12} {
		samples := make([][]any, n)
		for i := range samples {
			samples[i] = []any{end - int64(n-1-i)*30, fmt.Sprint(i)}
		}
		raw, _ := json.Marshal([]map[string]any{{"values": samples}})
		got := classifyMetricSamples(raw, end)
		want := "counter"
		if n < 4 || n > 11 {
			want = "unknown"
		}
		if got != want {
			t.Errorf("n=%d: %s", n, got)
		}
	}
	valid := `[{"values":[[1700000210,"1"],[1700000240,"2"],[1700000270,"3"],[1700000300,"4"]]}]`
	for _, raw := range []string{
		``, `null`, `{}`, `[]`, valid + ` {}`, `[{"values":null}]`,
		strings.Replace(valid, `"1"`, `"NaN"`, 1),
		strings.Replace(valid, `"1"`, `"+Inf"`, 1),
		strings.Replace(valid, `"1"`, `null`, 1),
		strings.Replace(valid, `"1"`, `1`, 1),
		strings.Replace(valid, `1700000210`, `1699999999`, 1),
		strings.Replace(valid, `1700000300`, `1700000301`, 1),
		strings.Replace(valid, `1700000240`, `1700000210`, 1),
		strings.Replace(valid, `1700000240`, `1700000220`, 1),
		strings.Replace(valid, `1700000240`, `1700000200`, 1),
		valid[:len(valid)-1] + `,` + valid[1:],
	} {
		if got := classifyMetricSamples([]byte(raw), end); got != "unknown" {
			t.Errorf("%s: %s", raw, got)
		}
	}
}
