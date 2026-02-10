package attributes

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"
)

const defaultTTL = 2 * time.Hour

// AttributeCache caches log and trace attribute names fetched from the API.
type AttributeCache struct {
	client      *http.Client
	cfg         models.Config
	logAttrs    []string
	traceAttrs  []string
	lastFetched time.Time
	ttl         time.Duration
	mu          sync.RWMutex
}

// NewAttributeCache creates a new AttributeCache.
func NewAttributeCache(client *http.Client, cfg models.Config) *AttributeCache {
	return &AttributeCache{
		client: client,
		cfg:    cfg,
		ttl:    defaultTTL,
	}
}

// Warm performs an initial best-effort fetch of both log and trace attributes.
func (c *AttributeCache) Warm(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	updated := false
	logAttrs, err := logs.FetchLogAttributeNames(ctx, c.client, c.cfg)
	if err != nil {
		log.Printf("Warning: failed to warm log attributes cache: %v", err)
	} else {
		c.logAttrs = logAttrs
		updated = true
	}

	traceAttrs, err := traces.FetchTraceAttributeNames(ctx, c.client, c.cfg)
	if err != nil {
		log.Printf("Warning: failed to warm trace attributes cache: %v", err)
	} else {
		c.traceAttrs = traceAttrs
		updated = true
	}

	if updated {
		c.lastFetched = time.Now()
	}
}

// GetLogAttributes returns cached log attribute names.
func (c *AttributeCache) GetLogAttributes() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	attrs := make([]string, len(c.logAttrs))
	copy(attrs, c.logAttrs)
	return attrs
}

// GetTraceAttributes returns cached trace attribute names.
func (c *AttributeCache) GetTraceAttributes() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	attrs := make([]string, len(c.traceAttrs))
	copy(attrs, c.traceAttrs)
	return attrs
}

// IsStale returns true if the cache is older than the TTL.
func (c *AttributeCache) IsStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.lastFetched) > c.ttl
}

// RefreshIfStale re-fetches attributes if the cache has expired.
// Returns nil on success, error otherwise.
func (c *AttributeCache) RefreshIfStale(ctx context.Context) error {
	if !c.IsStale() {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(c.lastFetched) <= c.ttl {
		return nil
	}

	logAttrs, err := logs.FetchLogAttributeNames(ctx, c.client, c.cfg)
	if err != nil {
		return err
	}

	traceAttrs, err := traces.FetchTraceAttributeNames(ctx, c.client, c.cfg)
	if err != nil {
		return err
	}

	c.logAttrs = logAttrs
	c.traceAttrs = traceAttrs
	c.lastFetched = time.Now()
	return nil
}
