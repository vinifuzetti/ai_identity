package spiffe

import (
	"context"

	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

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
