package types

import (
	"context"
	"github.com/chatton/celestia-test/framework/testutil/wait"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
)

type Chain interface {
	wait.ChainHeighter
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetHostRPCAddress() string
	GetGRPCAddress() string
	GetVolumeName() string // TODO: this should be removed and is a temporary function for docker only PoC.
	GetNodes() []Node
	AddNode(ctx context.Context, overrides map[string]any) error // TODO: use options pattern to allow for overrides.
}

type Node interface {
	GetType() string
	GetRPCClient() (rpcclient.Client, error)
	// GetInternalPeerAddress returns the peer address resolvable within the network.
	GetInternalPeerAddress(ctx context.Context) (string, error)
	// GetInternalRPCAddress returns the rpc address resolvable within the network.
	GetInternalRPCAddress(ctx context.Context) (string, error)
}
