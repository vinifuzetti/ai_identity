package spiffe

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// Client fornece acesso à Workload API do SPIRE Agent para obter JWT SVIDs.
type Client struct {
	wl *workloadapi.Client
}

func NewClient(ctx context.Context, socketPath string) (*Client, error) {
	wl, err := workloadapi.New(ctx, workloadapi.WithAddr("unix://"+socketPath))
	if err != nil {
		return nil, err
	}
	return &Client{wl: wl}, nil
}

func (c *Client) Close() {
	c.wl.Close()
}

func (c *Client) FetchJWTSVID(ctx context.Context, audience string) (*jwtsvid.SVID, error) {
	return c.wl.FetchJWTSVID(ctx, jwtsvid.Params{Audience: audience})
}

// NewX509Source cria uma fonte de X.509 SVIDs conectada à Workload API.
// Se preferredID não for vazio, seleciona o SVID com aquele SPIFFE ID como padrão
// (útil quando múltiplos SVIDs são retornados pelo agente).
func NewX509Source(ctx context.Context, socketPath, preferredID string) (*workloadapi.X509Source, error) {
	opts := []workloadapi.X509SourceOption{
		workloadapi.WithClientOptions(workloadapi.WithAddr("unix://" + socketPath)),
	}
	if preferredID != "" {
		opts = append(opts, workloadapi.WithDefaultX509SVIDPicker(
			func(svids []*x509svid.SVID) *x509svid.SVID {
				for _, s := range svids {
					if s.ID.String() == preferredID {
						return s
					}
				}
				return svids[0]
			},
		))
	}
	return workloadapi.NewX509Source(ctx, opts...)
}

// MTLSServerConfig retorna um *tls.Config para servidor mTLS SPIFFE.
// Aceita qualquer peer com SVID válido do trust domain especificado.
func MTLSServerConfig(source *workloadapi.X509Source, trustDomain string) (*tls.Config, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("trust domain inválido: %w", err)
	}
	return tlsconfig.MTLSServerConfig(source, source, tlsconfig.AuthorizeMemberOf(td)), nil
}

// MTLSClientConfig retorna um *tls.Config para cliente mTLS SPIFFE.
// Autentica apenas o servidor com o SPIFFE ID exato especificado.
func MTLSClientConfig(source *workloadapi.X509Source, serverSPIFFEID string) (*tls.Config, error) {
	id, err := spiffeid.FromString(serverSPIFFEID)
	if err != nil {
		return nil, fmt.Errorf("SPIFFE ID inválido: %w", err)
	}
	return tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(id)), nil
}
