package utils

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLockKeySerializesSameKey(t *testing.T) {
	unlock, err := LockKey(context.Background(), "test:same")
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func())
	go func() {
		second, err := LockKey(context.Background(), "test:same")
		if err != nil {
			t.Error(err)
			close(acquired)
			return
		}
		acquired <- second
	}()
	select {
	case <-acquired:
		t.Fatal("second LockKey acquired a held key")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case second := <-acquired:
		if second != nil {
			second()
		}
	case <-time.After(time.Second):
		t.Fatal("second LockKey did not acquire after unlock")
	}
}

func TestLockKeyDifferentKeysDoNotBlock(t *testing.T) {
	unlockA, err := LockKey(context.Background(), "test:a")
	if err != nil {
		t.Fatal(err)
	}
	defer unlockA()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlockB, err := LockKey(ctx, "test:b")
	if err != nil {
		t.Fatalf("different key blocked: %v", err)
	}
	unlockB()
}

func TestLockKeyWaitStopsAtContextEnd(t *testing.T) {
	unlock, err := LockKey(context.Background(), "test:ctx")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := LockKey(ctx, "test:ctx"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestFoldKeyMatchesEqualFold(t *testing.T) {
	names := []string{"High latency", " high latency ", "Σ latency", "σ latency", "ς latency",
		"K latency", "K latency", "k latency", "ß", "ẞ", "ss", "latency"}
	for _, a := range names {
		for _, b := range names {
			want := strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
			if got := FoldKey(a) == FoldKey(b); got != want {
				t.Errorf("FoldKey(%q) == FoldKey(%q) is %v, strings.EqualFold says %v", a, b, got, want)
			}
		}
	}
}
