package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// A client rotating toolset combinations must not force a build per request:
// every set it can name is built once, then served from the cache.
func TestToolsetServersBuildEveryRequestableSetOnce(t *testing.T) {
	c, b := newBlockingCache(t)
	b.gate = ""
	var names []string
	for _, name := range toolsets.ValidNames() {
		if name != "all" {
			names = append(names, name)
		}
	}
	sets := map[string]toolsets.Set{}
	for range 2 {
		for mask := 1; mask < 1<<len(names); mask++ {
			var spec []string
			for i, name := range names {
				if mask&(1<<i) != 0 {
					spec = append(spec, name)
				}
			}
			allowed, err := toolsets.Parse(strings.Join(spec, ","))
			if err != nil {
				t.Fatal(err)
			}
			sets[toolSetKey(allowed)] = allowed
			handlerWithin(t, c, allowed)
		}
	}
	for key, allowed := range sets {
		if n := b.count(allowed); n != 1 {
			t.Errorf("built %q %d times, want 1", key, n)
		}
	}
	// The cache's bound: one entry per set a request can name, never more.
	if len(c.cached) != len(sets) {
		t.Fatalf("cache holds %d servers for %d requestable sets", len(c.cached), len(sets))
	}
}

// blockingBuilds builds a bare server per set; a build for gate's tool waits
// until release is closed, after signalling started.
type blockingBuilds struct {
	gate     string
	started  chan struct{}
	release  chan struct{}
	opened   sync.Once
	failNext atomic.Bool
	builds   sync.Map // tool set key -> *atomic.Int32
}

func (b *blockingBuilds) build(allowed toolsets.Set) (*last9mcp.Last9MCPServer, error) {
	n, _ := b.builds.LoadOrStore(toolSetKey(allowed), &atomic.Int32{})
	n.(*atomic.Int32).Add(1)
	if _, ok := allowed[b.gate]; ok {
		b.started <- struct{}{}
		<-b.release
	}
	if b.failNext.Swap(false) {
		return nil, errors.New("build failed")
	}
	return last9mcp.NewServerWithOptions("test", "0", last9mcp.WithSkipProviderInit())
}

func (b *blockingBuilds) open() { b.opened.Do(func() { close(b.release) }) }

func (b *blockingBuilds) count(allowed toolsets.Set) int32 {
	n, ok := b.builds.Load(toolSetKey(allowed))
	if !ok {
		return 0
	}
	return n.(*atomic.Int32).Load()
}

func newBlockingCache(t *testing.T) (*toolsetServers, *blockingBuilds) {
	t.Helper()
	b := &blockingBuilds{gate: "get_logs", started: make(chan struct{}, 8), release: make(chan struct{})}
	c := NewHTTPServer(nil, models.Config{}, b.build).servers
	t.Cleanup(func() {
		b.open()
		_ = c.shutdown(context.Background())
	})
	return c, b
}

func handlerWithin(t *testing.T, c *toolsetServers, allowed toolsets.Set) http.Handler {
	t.Helper()
	type result struct {
		h   http.Handler
		err error
	}
	done := make(chan result, 1)
	go func() {
		h, err := c.handler(allowed)
		done <- result{h, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.h
	case <-time.After(5 * time.Second):
		t.Fatalf("handler(%v) blocked behind another set's build", allowed)
		return nil
	}
}

func TestToolsetServersBuildOutsideTheLock(t *testing.T) {
	c, b := newBlockingCache(t)
	logs := toolsets.Set{"get_logs": {}}
	traces := toolsets.Set{"get_traces": {}}
	metrics := toolsets.Set{"prometheus_range_query": {}}
	handlerWithin(t, c, traces)

	slow := make(chan error, 1)
	go func() {
		_, err := c.handler(logs)
		slow <- err
	}()
	<-b.started

	handlerWithin(t, c, traces)
	handlerWithin(t, c, metrics)
	b.open()
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
}

func TestToolsetServersBuildEachSetOnce(t *testing.T) {
	c, b := newBlockingCache(t)
	logs := toolsets.Set{"get_logs": {}}
	handlers := make(chan http.Handler, 2)
	for range 2 {
		go func() {
			h, err := c.handler(logs)
			if err != nil {
				t.Error(err)
			}
			handlers <- h
		}()
	}
	<-b.started
	time.Sleep(50 * time.Millisecond)
	b.open()
	first, second := <-handlers, <-handlers
	if first == nil || first != second {
		t.Fatalf("concurrent misses got handlers %p and %p, want one shared handler", first, second)
	}
	if n := b.count(logs); n != 1 {
		t.Fatalf("built logs %d times, want 1", n)
	}
}

func TestToolsetServersRetryAFailedBuild(t *testing.T) {
	c, b := newBlockingCache(t)
	traces := toolsets.Set{"get_traces": {}}
	b.failNext.Store(true)
	if _, err := c.handler(traces); err == nil {
		t.Fatal("want the build error")
	}
	handlerWithin(t, c, traces)
	if n := b.count(traces); n != 2 {
		t.Fatalf("built traces %d times, want a retry after the failure", n)
	}
}
