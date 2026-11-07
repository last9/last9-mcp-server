package utils

import (
	"context"
	"errors"
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
