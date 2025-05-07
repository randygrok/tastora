package docker

import (
	"context"
	"github.com/chatton/celestia-test/framework/types"
)

var _ types.ChainProvider = &Provider{}

type Provider struct {
	cfg      Config
	testName string
}

func (p *Provider) GetChain(ctx context.Context) (types.Chain, error) {
	return newChain(ctx, p.testName, p.cfg)
}

func NewProvider(cfg Config, testName string) *Provider {
	return &Provider{
		cfg:      cfg,
		testName: testName,
	}
}
