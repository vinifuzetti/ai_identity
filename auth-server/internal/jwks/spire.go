package jwks

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// BundleFile implementa jwtbundle.Source lendo o JWKS de um arquivo exportado
// periodicamente pelo SPIRE Server via `spire-server bundle show -format jwks`.
//
// Elimina a necessidade de um SPIRE Agent co-localizado no Auth Server:
// o bundle é informação pública — apenas as chaves de verificação do trust domain.
type BundleFile struct {
	mu          sync.RWMutex
	bundle      *jwtbundle.Bundle
	lastUpdate  time.Time
	path        string
	trustDomain spiffeid.TrustDomain
	ttl         time.Duration
}

func NewBundleFile(path, trustDomain string) (*BundleFile, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("trust domain inválido: %w", err)
	}
	return &BundleFile{
		path:        path,
		trustDomain: td,
		ttl:         60 * time.Second,
	}, nil
}

// GetJWTBundleForTrustDomain implementa jwtbundle.Source — usado por jwtsvid.ParseAndValidate.
func (b *BundleFile) GetJWTBundleForTrustDomain(td spiffeid.TrustDomain) (*jwtbundle.Bundle, error) {
	if td != b.trustDomain {
		return nil, fmt.Errorf("trust domain desconhecido: %s", td)
	}

	b.mu.RLock()
	if b.bundle != nil && time.Since(b.lastUpdate) < b.ttl {
		bundle := b.bundle
		b.mu.RUnlock()
		return bundle, nil
	}
	b.mu.RUnlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	data, err := os.ReadFile(b.path)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler bundle JWKS (%s): %w", b.path, err)
	}

	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(data, &jwks); err != nil {
		return nil, fmt.Errorf("falha ao parsear JWKS: %w", err)
	}

	bundle := jwtbundle.New(b.trustDomain)
	for _, key := range jwks.Keys {
		if err := bundle.AddJWTAuthority(key.KeyID, key.Public()); err != nil {
			return nil, fmt.Errorf("falha ao adicionar chave %s: %w", key.KeyID, err)
		}
	}

	b.bundle = bundle
	b.lastUpdate = time.Now()
	return bundle, nil
}
