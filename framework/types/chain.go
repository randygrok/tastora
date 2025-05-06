package types

import "context"

type Chain interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	// TODO: remainder of functions needed.
}
