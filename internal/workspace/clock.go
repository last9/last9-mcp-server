// Package workspace holds orientation tools: what time it is and what the
// workspace contains.
package workspace

import (
	"strings"
	"time"

	"last9-mcp/internal/utils"
)

// Instant is one point in time in every form a tool argument may need.
type Instant struct {
	ISO          string `json:"iso"`
	EpochSeconds int64  `json:"epoch_seconds"`
	Local        string `json:"local,omitempty"`
}

func newInstant(t time.Time, loc *time.Location) Instant {
	in := Instant{ISO: t.UTC().Format(time.RFC3339), EpochSeconds: t.Unix()}
	if loc != time.UTC {
		in.Local = t.In(loc).Format(time.RFC3339)
	}
	return in
}

// CurrentTime is the get_current_time payload; get_workspace_overview embeds it.
type CurrentTime struct {
	Now      Instant  `json:"now"`
	Timezone string   `json:"timezone"`
	Range    string   `json:"range,omitempty"`
	Start    *Instant `json:"start,omitempty"`
	End      *Instant `json:"end,omitempty"`
}

// resolveCurrentTime reads now in timezone and, when expr is set, resolves it
// to absolute bounds.
func resolveCurrentTime(now time.Time, expr, timezone string) (CurrentTime, error) {
	loc, err := utils.ResolveTimeZone(timezone)
	if err != nil {
		return CurrentTime{}, err
	}
	out := CurrentTime{Now: newInstant(now, loc), Timezone: loc.String()}
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return out, nil
	}
	start, end, err := utils.ParseTimeRangeExpr(expr, now, loc)
	if err != nil {
		return CurrentTime{}, err
	}
	// An inline zone in expr ("since 9am IST") wins for the bounds; local
	// renderings stay in the requested zone so all three read alike.
	s, e := newInstant(start, loc), newInstant(end, loc)
	out.Range, out.Start, out.End = expr, &s, &e
	return out, nil
}
