package types

import "context"

type Header struct {
	Height uint64 `json:"height"`
}

type Node interface {
	// Start starts the node.
	Start(ctx context.Context, coreIp, genesisBlockHash string) error
	// Stop stops the node.
	Stop(ctx context.Context) error
	// GetType returns the type of node. E.g. "bridge" / "light" / "full"
	GetType() string
	// GetHeader returns a header at a specified height.
	GetHeader(ctx context.Context, height uint64) (Header, error)
}
