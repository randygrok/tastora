package types

type ChainProvider interface {
	GetChain() (Chain, error)
}
