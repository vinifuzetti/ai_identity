package jwks

import (
	"context"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// SPIRECache busca e armazena em cache o bundle JWT do SPIRE via Workload API.
// O bundle é informação pública — não requer registro de workload no SPIRE Agent.
type SPIRECache struct {
	mu         sync.RWMutex
	bundles    *jwtbundle.Set
	lastUpdate time.Time
	socketPath string
	ttl        time.Duration
}

func NewSPIRECache(socketPath string) *SPIRECache {
	return &SPIRECache{
		socketPath: socketPath,
		ttl:        5 * time.Minute,
	}
}

// GetBundles retorna o bundle JWT do trust domain, usando cache com TTL.
func (c *SPIRECache) GetBundles(ctx context.Context) (*jwtbundle.Set, error) {
	c.mu.RLock()
	if c.bundles != nil && time.Since(c.lastUpdate) < c.ttl {
		b := c.bundles
		c.mu.RUnlock()
		return b, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	client, err := workloadapi.New(ctx, workloadapi.WithAddr("unix://"+c.socketPath))
	if err != nil {
		return nil, err
	}
	defer client.Close()

	bundles, err := client.FetchJWTBundles(ctx)
	if err != nil {
		return nil, err
	}

	c.bundles = bundles
	c.lastUpdate = time.Now()
	return bundles, nil
}
