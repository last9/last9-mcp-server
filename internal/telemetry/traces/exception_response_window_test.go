package traces

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestOccurrenceTimesStayInsideReportedWindow(t *testing.T) {
	for _, fraction := range []time.Duration{0, 900 * time.Millisecond} {
		t.Run(fraction.String(), func(t *testing.T) {
			requestedWindow := exceptionWindow{
				start: exceptionTestWindow.start.Add(fraction),
				end:   exceptionTestWindow.end.Add(fraction),
			}
			window := exceptionWindow{start: requestedWindow.start.Truncate(time.Second), end: requestedWindow.end.Truncate(time.Second)}
			if window.start.Before(requestedWindow.start) {
				window.start = window.start.Add(time.Second)
			}
			first := window.start.Add(time.Minute)
			last := window.end
			response := runGetExceptionsResponse(t, getExceptionsTestRequest{
				InstantBody: twoEnvRows,
				Window:      requestedWindow,
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
			rowURL, err := url.Parse(row["url"].(string))
			if err != nil {
				t.Fatalf("parse row URL: %v", err)
			}
			from, err := strconv.ParseInt(rowURL.Query().Get("from"), 10, 64)
			if err != nil {
				t.Fatalf("parse row URL from: %v", err)
			}
			to, err := strconv.ParseInt(rowURL.Query().Get("to"), 10, 64)
			if err != nil {
				t.Fatalf("parse row URL to: %v", err)
			}
			if first.Before(time.Unix(from, 0)) || last.After(time.Unix(to, 0)) {
				t.Fatalf("row URL window %s / %s does not contain occurrences %s / %s", time.Unix(from, 0), time.Unix(to, 0), first, last)
			}
		})
	}
}
