package types

import "context"

// Provider is an interface to implement chain and node provisioning functionality.
// It defines methods to retrieve instances of Chain and Node.
//
// different Providers can be implement to enable different infrastructure / bankends for the Chains and Nodes to run
// on.
type Provider interface {
	// GetChain returns an implement of the Chain interface.
	GetChain(ctx context.Context) (Chain, error)
	// GetNode returns an implementation of the Node interface.
	GetNode(ctx context.Context, nodeType string) (Node, error)
}
