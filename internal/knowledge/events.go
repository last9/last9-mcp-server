package knowledge

import (
	"time"
)

// GetTimeBucket returns the start and end of the 5-minute bucket for a given timestamp
func GetTimeBucket(t time.Time) (time.Time, time.Time) {
	if t.IsZero() {
		t = time.Now()
	}
	bucketSize := 5 * time.Minute
	start := t.Truncate(bucketSize)
	end := start.Add(bucketSize)
	return start, end
}
