package utils

import (
	"context"
	"strings"
	"sync"
	"unicode"
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

// FoldKey returns a lock key part that is equal for two strings exactly when
// strings.EqualFold reports their trimmed forms equal, so one lock covers every
// spelling a case-insensitive comparison treats as the same name.
func FoldKey(s string) string {
	return strings.Map(foldRune, strings.TrimSpace(s))
}

// foldRune maps r to the smallest rune in its case-folding orbit (Σ, σ and ς all map to Σ).
func foldRune(r rune) rune {
	smallest := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		smallest = min(smallest, f)
	}
	return smallest
}
