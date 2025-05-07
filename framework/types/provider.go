package types

import "context"

type ChainProvider interface {
	GetChain(ctx context.Context) (Chain, error)
}
