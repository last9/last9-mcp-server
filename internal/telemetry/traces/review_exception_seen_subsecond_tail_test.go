package traces

import (
	"testing"
	"time"
)

func TestReviewPrometheusDurationPreservesSubsecondTail(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{time.Minute, "1m"},
		{500 * time.Millisecond, "500ms"},
		{60*time.Second + 500*time.Millisecond, "60500ms"},
	} {
		if got := prometheusDuration(tc.in); got != tc.want {
			t.Errorf("prometheusDuration(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
