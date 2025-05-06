package docker

import "github.com/chatton/celestia-test/framework/types"

var _ types.ChainProvider = &Provider{}

type Provider struct{}

func (p *Provider) GetChain() (types.Chain, error) {
	panic("implement me")
}

func NewProvider() *Provider {
	return &Provider{}
}
