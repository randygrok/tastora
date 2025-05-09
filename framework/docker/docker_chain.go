package docker

import (
	"bytes"
	"context"
	sdkmath "cosmossdk.io/math"
	"fmt"
	"github.com/chatton/celestia-test/framework/docker/consts"
	addressutil "github.com/chatton/celestia-test/framework/testutil/address"
	"github.com/chatton/celestia-test/framework/testutil/toml"
	"github.com/chatton/celestia-test/framework/testutil/wait"
	"github.com/chatton/celestia-test/framework/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	dockerimagetypes "github.com/docker/docker/api/types/image"
	volumetypes "github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"
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

var sentryPorts = nat.PortMap{
	nat.Port(p2pPort):     {},
	nat.Port(rpcPort):     {},
	nat.Port(grpcPort):    {},
	nat.Port(apiPort):     {},
	nat.Port(privValPort): {},
}

func newChain(ctx context.Context, testName string, cfg Config) (types.Chain, error) {
	if cfg.ChainConfig == nil {
		return nil, fmt.Errorf("chain config must be set")
	}
	if cfg.ChainConfig.EncodingConfig == nil {
		return nil, fmt.Errorf("chain config must have an encoding config set")
	}

	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	kr := keyring.NewInMemory(cdc)

	// If unspecified, NumValidators defaults to 2 and NumFullNodes defaults to 1.
	if cfg.ChainConfig.NumValidators == nil {
		nv := defaultNumValidators
		cfg.ChainConfig.NumValidators = &nv
	}
	if cfg.ChainConfig.NumFullNodes == nil {
		nf := defaultNumFullNodes
		cfg.ChainConfig.NumFullNodes = &nf
	}

	c := &Chain{
		testName: testName,
		cfg:      cfg,
		cdc:      cdc,
		keyring:  kr,
		log:      cfg.Logger,
	}

	// create the underlying docker resources for the chain.
	if err := c.initializeChainNodes(ctx, testName); err != nil {
		return nil, err
	}

	return c, nil
}

type Chain struct {
	testName     string
	cfg          Config
	Validators   ChainNodes
	FullNodes    ChainNodes
	numFullNodes int
	cdc          *codec.ProtoCodec
	log          *zap.Logger
	keyring      keyring.Keyring
	findTxMu     sync.Mutex
}

func (c *Chain) AddNode(ctx context.Context, overrides map[string]any) error {
	return c.AddFullNodes(ctx, overrides, 1)
}

// AddFullNodes adds new fullnodes to the network, peering with the existing nodes.
func (c *Chain) AddFullNodes(ctx context.Context, configFileOverrides map[string]any, inc int) error {
	// Get peer string for existing nodes

	peers, err := addressutil.BuildInternalPeerAddressList(ctx, c.GetNodes())
	if err != nil {
		return err
	}

	// Get genesis.json
	genbz, err := c.Validators[0].GenesisFileContent(ctx)
	if err != nil {
		return err
	}

	prevCount := c.numFullNodes
	c.numFullNodes += inc
	if err := c.initializeChainNodes(ctx, c.testName); err != nil {
		return err
	}

	var eg errgroup.Group
	for i := prevCount; i < c.numFullNodes; i++ {
		eg.Go(func() error {
			fn := c.FullNodes[i]
			if err := fn.InitFullNodeFiles(ctx); err != nil {
				return err
			}
			if err := fn.SetPeers(ctx, peers); err != nil {
				return err
			}
			if err := fn.OverwriteGenesisFile(ctx, genbz); err != nil {
				return err
			}
			for configFile, modifiedConfig := range configFileOverrides {
				modifiedToml, ok := modifiedConfig.(toml.Toml)
				if !ok {
					return fmt.Errorf("provided toml override for file %s is of type (%T). Expected (DecodedToml)", configFile, modifiedConfig)
				}
				if err := ModifyConfigFile(
					ctx,
					fn.logger(),
					fn.DockerClient,
					fn.TestName,
					fn.VolumeName,
					configFile,
					modifiedToml,
				); err != nil {
					return err
				}
			}
			if err := fn.CreateNodeContainer(ctx); err != nil {
				return err
			}
			return fn.StartContainer(ctx)
		})
	}
	return eg.Wait()
}

func (c *Chain) GetNodes() []types.ChainNode {
	var nodes []types.ChainNode
	for _, n := range c.Nodes() {
		nodes = append(nodes, n)
	}
	return nodes
}

func (c *Chain) GetGRPCAddress() string {
	return fmt.Sprintf("%s:9090", c.GetNode().HostName())
}

func (c *Chain) GetVolumeName() string {
	return c.GetNode().VolumeName
}

// GetHostRPCAddress returns the address of the RPC server accessible by the host.
// This will not return a valid address until the chain has been started.
func (c *Chain) GetHostRPCAddress() string {
	return "http://" + c.GetNode().hostRPCPort
}

func (c *Chain) Height(ctx context.Context) (int64, error) {
	return c.GetNode().Height(ctx)
}

func (c *Chain) Start(ctx context.Context) error {
	cfg := c.cfg

	genesisAmounts := make([][]sdk.Coin, len(c.Validators))
	genesisSelfDelegation := make([]sdk.Coin, len(c.Validators))

	for i := range c.Validators {
		genesisAmounts[i] = []sdk.Coin{{Amount: sdkmath.NewInt(10_000_000_000_000), Denom: cfg.ChainConfig.Denom}}
		genesisSelfDelegation[i] = sdk.Coin{Amount: sdkmath.NewInt(5_000_000), Denom: cfg.ChainConfig.Denom}
	}

	configFileOverrides := cfg.ChainConfig.ConfigFileOverrides

	eg := new(errgroup.Group)
	// Initialize config and sign gentx for each validator.
	for i, v := range c.Validators {
		v.Validator = true
		eg.Go(func() error {
			if err := v.InitFullNodeFiles(ctx); err != nil {
				return err
			}
			for configFile, modifiedConfig := range configFileOverrides {
				modifiedToml, ok := modifiedConfig.(toml.Toml)
				if !ok {
					return fmt.Errorf("provided toml override for file %s is of type (%T). Expected (DecodedToml)", configFile, modifiedConfig)
				}
				if err := ModifyConfigFile(
					ctx,
					c.cfg.Logger,
					v.DockerClient,
					v.TestName,
					v.VolumeName,
					configFile,
					modifiedToml,
				); err != nil {
					return fmt.Errorf("failed to modify toml config file: %w", err)
				}
			}
			return v.InitValidatorGenTx(ctx, genesisAmounts[i], genesisSelfDelegation[i])
		})
	}

	// Initialize config for each full node.
	for _, n := range c.FullNodes {
		n.Validator = false
		eg.Go(func() error {
			if err := n.InitFullNodeFiles(ctx); err != nil {
				return err
			}
			for configFile, modifiedConfig := range configFileOverrides {
				modifiedToml, ok := modifiedConfig.(toml.Toml)
				if !ok {
					return fmt.Errorf("provided toml override for file %s is of type (%T). Expected (DecodedToml)", configFile, modifiedConfig)
				}
				if err := ModifyConfigFile(
					ctx,
					n.logger(),
					n.DockerClient,
					n.TestName,
					n.VolumeName,
					configFile,
					modifiedToml,
				); err != nil {
					return err
				}
			}
			return nil
		})
	}

	// wait for this to finish
	if err := eg.Wait(); err != nil {
		return err
	}

	// for the validators we need to collect the gentxs and the accounts
	// to the first node's genesis file
	validator0 := c.Validators[0]
	for i := 1; i < len(c.Validators); i++ {
		validatorN := c.Validators[i]

		bech32, err := validatorN.AccountKeyBech32(ctx, valKey)
		if err != nil {
			return err
		}

		if err := validator0.AddGenesisAccount(ctx, bech32, genesisAmounts[0]); err != nil {
			return err
		}

		if err := validatorN.copyGentx(ctx, validator0); err != nil {
			return err
		}
	}

	if err := validator0.CollectGentxs(ctx); err != nil {
		return err
	}

	genbz, err := validator0.GenesisFileContent(ctx)
	if err != nil {
		return err
	}

	genbz = bytes.ReplaceAll(genbz, []byte(`"stake"`), []byte(fmt.Sprintf(`"%s"`, cfg.ChainConfig.Denom)))

	if c.cfg.ChainConfig.ModifyGenesis != nil {
		genbz, err = c.cfg.ChainConfig.ModifyGenesis(cfg, genbz)
		if err != nil {
			return err
		}
	}

	chainNodes := c.Nodes()

	for _, cn := range chainNodes {
		if err := cn.OverwriteGenesisFile(ctx, genbz); err != nil {
			return err
		}
	}

	eg, egCtx := errgroup.WithContext(ctx)
	for _, n := range chainNodes {
		eg.Go(func() error {
			return n.CreateNodeContainer(egCtx)
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	typedNodes := make([]types.ChainNode, len(chainNodes))
	for i, n := range chainNodes {
		typedNodes[i] = n
	}

	peers, err := addressutil.BuildInternalPeerAddressList(ctx, typedNodes)
	if err != nil {
		return err
	}

	eg, egCtx = errgroup.WithContext(ctx)
	for _, n := range chainNodes {
		c.log.Info("Starting container", zap.String("container", n.Name()), zap.String("peers", peers))
		eg.Go(func() error {
			if err := n.SetPeers(egCtx, peers); err != nil {
				return err
			}
			return n.StartContainer(egCtx)
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	// Wait for blocks before considering the chains "started"
	return wait.ForBlocks(ctx, 2, c.GetNode())
}

func (c *Chain) GetNode() *ChainNode {
	return c.Validators[0]
}

// Nodes returns all nodes, including validators and fullnodes.
func (c *Chain) Nodes() ChainNodes {
	return append(c.Validators, c.FullNodes...)
}

func (c *Chain) Stop(ctx context.Context) error {
	var eg errgroup.Group
	for _, n := range c.Nodes() {
		eg.Go(func() error {
			return n.RemoveContainer(ctx)
		})
	}
	return eg.Wait()
}

// creates the test node objects required for bootstrapping tests.
func (c *Chain) initializeChainNodes(
	ctx context.Context,
	testName string,
) error {

	numValidators := *c.cfg.ChainConfig.NumValidators

	chainCfg := c.cfg
	c.pullImages(ctx)
	image := chainCfg.ChainConfig.Images[0]

	newVals := make(ChainNodes, numValidators)
	copy(newVals, c.Validators)
	newFullNodes := make(ChainNodes, c.numFullNodes)
	copy(newFullNodes, c.FullNodes)

	eg, egCtx := errgroup.WithContext(ctx)
	for i := len(c.Validators); i < numValidators; i++ {
		eg.Go(func() error {
			val, err := c.newChainNode(egCtx, testName, image, true, i)
			if err != nil {
				return err
			}
			newVals[i] = val
			return nil
		})
	}
	for i := len(c.FullNodes); i < c.numFullNodes; i++ {
		eg.Go(func() error {
			fn, err := c.newChainNode(egCtx, testName, image, false, i)
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

// newChainNode constructs a new cosmos chain node with a docker volume.
func (c *Chain) newChainNode(
	ctx context.Context,
	testName string,
	image DockerImage,
	validator bool,
	index int,
) (*ChainNode, error) {
	// Construct the ChainNode first so we can access its name.
	// The ChainNode's VolumeName cannot be set until after we create the volume.
	tn := NewDockerChainNode(c.log, validator, c.cfg, testName, image, index)

	v, err := c.cfg.DockerClient.VolumeCreate(ctx, volumetypes.CreateOptions{
		Labels: map[string]string{
			consts.CleanupLabel:   testName,
			consts.NodeOwnerLabel: tn.Name(),
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
	for _, image := range c.cfg.ChainConfig.Images {
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
