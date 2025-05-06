package docker

import "github.com/chatton/celestia-test/framework/types"

var _ types.ChainProvider = &Provider{}

type Provider struct {
	cfg Config
}

func (p *Provider) GetChain() (types.Chain, error) {

	panic("implement me")
}

func NewProvider(cfg Config) *Provider {
	return &Provider{
		cfg: cfg,
	}
}
