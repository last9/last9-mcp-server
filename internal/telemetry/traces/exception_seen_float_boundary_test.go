package traces

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestOccurrenceBoundariesSurviveFloatRoundTrip(t *testing.T) {
	base := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	for _, fraction := range []time.Duration{0, 5 * time.Millisecond, 6 * time.Millisecond} {
		t.Run(fraction.String(), func(t *testing.T) {
			start := base.Add(fraction)
			inside := start.Add(time.Minute)
			end := start.Add(time.Hour)
			rows := runGetExceptions(t, getExceptionsTestRequest{
				InstantBody: twoEnvRows,
				Window:      exceptionWindow{start: start, end: end},
				RangeHandler: func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintf(w, `[{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"values":[[%.3f,"1"],[%.3f,"1"],[%.3f,"1"]]}]`,
						float64(start.UnixNano())/float64(time.Second),
						float64(inside.UnixNano())/float64(time.Second),
						float64(end.UnixNano())/float64(time.Second))
				},
			})
			if got := rows[0]["first_seen"]; got != inside.Format(time.RFC3339Nano) {
				t.Fatalf("first_seen = %v, want %s", got, inside.Format(time.RFC3339Nano))
			}
			if got := rows[0]["last_seen"]; got != end.Format(time.RFC3339Nano) {
				t.Fatalf("last_seen = %v, want %s", got, end.Format(time.RFC3339Nano))
			}
		})
	}
}
