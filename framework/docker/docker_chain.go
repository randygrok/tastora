package docker

import (
	"context"
	"fmt"
	"github.com/chatton/celestia-test/framework/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	dockerimagetypes "github.com/docker/docker/api/types/image"
	volumetypes "github.com/docker/docker/api/types/volume"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"io"
	"sync"
)

var _ types.Chain = &Chain{}

const (
	defaultNumValidators = 2
	defaultNumFullNodes  = 1
)

func NewChain(ctx context.Context, testName string, cfg Config) (types.Chain, error) {
	if cfg.EncodingConfig == nil {
		panic("chain config must have an encoding config set")
	}

	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	kr := keyring.NewInMemory(cdc)

	// If unspecified, NumValidators defaults to 2 and NumFullNodes defaults to 1.
	if cfg.NumValidators == nil {
		nv := defaultNumValidators
		cfg.NumValidators = &nv
	}
	if cfg.NumFullNodes == nil {
		nf := defaultNumFullNodes
		cfg.NumFullNodes = &nf
	}

	c := &Chain{
		testName: testName,
		cfg:      cfg,
		cdc:      cdc,
		keyring:  kr,
	}

	// create the underlying docker resources for the chain.
	if err := c.initializeChainNodes(ctx, testName); err != nil {
		return nil, err
	}

	return c, nil
}

type Chain struct {
	testName   string
	cfg        Config
	Validators ChainNodes
	FullNodes  ChainNodes
	cdc        *codec.ProtoCodec
	log        *zap.Logger
	keyring    keyring.Keyring
	findTxMu   sync.Mutex
}

func (c *Chain) Start(ctx context.Context) error {
	panic("implement me")
}

func (c *Chain) Stop(ctx context.Context) error {
	panic("implement me")
}

// creates the test node objects required for bootstrapping tests.
func (c *Chain) initializeChainNodes(
	ctx context.Context,
	testName string,
) error {

	numValidators := *c.cfg.NumValidators
	numFullNodes := *c.cfg.NumFullNodes

	chainCfg := c.cfg
	c.pullImages(ctx)
	image := chainCfg.Images[0]

	newVals := make(ChainNodes, numValidators)
	copy(newVals, c.Validators)
	newFullNodes := make(ChainNodes, numFullNodes)
	copy(newFullNodes, c.FullNodes)

	eg, egCtx := errgroup.WithContext(ctx)
	for i := len(c.Validators); i < numValidators; i++ {
		eg.Go(func() error {
			val, err := c.NewChainNode(egCtx, testName, image, true, i)
			if err != nil {
				return err
			}
			newVals[i] = val
			return nil
		})
	}
	for i := len(c.FullNodes); i < numFullNodes; i++ {
		eg.Go(func() error {
			fn, err := c.NewChainNode(egCtx, testName, image, false, i)
			if err != nil {
				return err
			}
			newFullNodes[i] = fn
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	c.findTxMu.Lock()
	defer c.findTxMu.Unlock()
	c.Validators = newVals
	c.FullNodes = newFullNodes
	return nil
}

// NewChainNode constructs a new cosmos chain node with a docker volume.
func (c *Chain) NewChainNode(
	ctx context.Context,
	testName string,
	image Image,
	validator bool,
	index int,
) (*ChainNode, error) {
	// Construct the ChainNode first so we can access its name.
	// The ChainNode's VolumeName cannot be set until after we create the volume.
	tn := NewDockerChainNode(c.log, validator, c.cfg, testName, image, index)

	v, err := c.cfg.DockerClient.VolumeCreate(ctx, volumetypes.CreateOptions{
		Labels: map[string]string{
			CleanupLabel:   testName,
			NodeOwnerLabel: tn.Name(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating volume for chain node: %w", err)
	}
	tn.VolumeName = v.Name

	if err := SetVolumeOwner(ctx, VolumeOwnerOptions{
		Log:        c.log,
		Client:     c.cfg.DockerClient,
		VolumeName: v.Name,
		ImageRef:   image.Ref(),
		TestName:   testName,
		UidGid:     image.UIDGID,
	}); err != nil {
		return nil, fmt.Errorf("set volume owner: %w", err)
	}

	return tn, nil
}

func (c *Chain) pullImages(ctx context.Context) {
	for _, image := range c.cfg.Images {
		if image.Version == "local" {
			continue
		}
		rc, err := c.cfg.DockerClient.ImagePull(
			ctx,
			image.Repository+":"+image.Version,
			dockerimagetypes.PullOptions{},
		)
		if err != nil {
			c.log.Error("Failed to pull image",
				zap.Error(err),
				zap.String("repository", image.Repository),
				zap.String("tag", image.Version),
			)
		} else {
			_, _ = io.Copy(io.Discard, rc)
			_ = rc.Close()
		}
	}
}
