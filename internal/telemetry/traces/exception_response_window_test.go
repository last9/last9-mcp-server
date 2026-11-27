package traces

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestOccurrenceTimesStayInsideReportedWindow(t *testing.T) {
	for _, fraction := range []time.Duration{0, 900 * time.Millisecond} {
		t.Run(fraction.String(), func(t *testing.T) {
			window := exceptionWindow{
				start: exceptionTestWindow.start.Add(fraction),
				end:   exceptionTestWindow.end.Add(fraction),
			}
			first := window.start.Add(time.Minute)
			last := window.end
			response := runGetExceptionsResponse(t, getExceptionsTestRequest{
				InstantBody: twoEnvRows,
				Window:      window,
				RangeHandler: func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintf(w, `[{"metric":{"exception_type":"TypeError","service_name":"web","span_name":"View","span_kind":"SPAN_KIND_SERVER","env":"prod"},"values":[[%f,"1"],[%f,"1"]]}]`,
						float64(first.UnixNano())/float64(time.Second), float64(last.UnixNano())/float64(time.Second))
				},
			})

			if response.StartTime != window.start.Format(time.RFC3339Nano) || response.EndTime != window.end.Format(time.RFC3339Nano) {
				t.Fatalf("reported window = %s / %s, want %s / %s", response.StartTime, response.EndTime, window.start.Format(time.RFC3339Nano), window.end.Format(time.RFC3339Nano))
			}
			row := response.Exceptions[0]
			if row["first_seen"] != first.Format(time.RFC3339Nano) || row["last_seen"] != last.Format(time.RFC3339Nano) {
				t.Fatalf("first/last seen = %v / %v, want %s / %s", row["first_seen"], row["last_seen"], first.Format(time.RFC3339Nano), last.Format(time.RFC3339Nano))
			}
		})
	}
}
