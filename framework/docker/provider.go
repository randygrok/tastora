package docker

import (
	"context"
	"github.com/chatton/celestia-test/framework/types"
	"testing"
)

var _ types.Provider = &Provider{}

type Provider struct {
	t   *testing.T
	cfg Config
}

// GetNode retrieves a node of the specified type. Only "bridge" node type is supported; otherwise, it panics.
func (p *Provider) GetNode(ctx context.Context, nodeType string) (types.Node, error) {
	if nodeType != "bridge" {
		panic("only bridge node type is supported")
	}
	return newBridgeNode(ctx, p.t.Name(), p.cfg)
}

// GetChain returns an initialized Chain instance based on the provided configuration and test name context.
// It creates necessary underlying resources and validates the configuration before instantiating the Chain.
func (p *Provider) GetChain(ctx context.Context) (types.Chain, error) {
	return newChain(ctx, p.t, p.cfg)
}

// NewProvider creates and returns a new Provider instance using the provided configuration and test name.
func NewProvider(cfg Config, t *testing.T) *Provider {
	return &Provider{
		cfg: cfg,
		t:   t,
	}
}
