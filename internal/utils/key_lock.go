package utils

import (
	"context"
	"sync"
)

// Keys are never evicted: there is one per resource a write tool touched in this
// process (dashboard, alert rule, rule name), a few bytes each.
var keyLocks sync.Map

// LockKey serializes callers holding the same key within this process, so a
// read-then-write tool cannot interleave with another call on the same resource.
// Waiting stops with ctx's error when ctx ends first.
func LockKey(ctx context.Context, key string) (unlock func(), err error) {
	value, _ := keyLocks.LoadOrStore(key, make(chan struct{}, 1))
	slot := value.(chan struct{})
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
