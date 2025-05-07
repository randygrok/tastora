package docker

import (
	"bytes"
	"context"
	sdkmath "cosmossdk.io/math"
	"fmt"
	"github.com/avast/retry-go/v4"
	"github.com/chatton/celestia-test/framework/testutil/toml"
	"github.com/chatton/celestia-test/framework/testutil/wait"
	"github.com/chatton/celestia-test/framework/types"
	tmjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/p2p"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	libclient "github.com/cometbft/cometbft/rpc/jsonrpc/client"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"sync"
	"time"
)

var _ types.Chain = &Chain{}
var _ wait.ChainHeighter = &Chain{}

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

func (c *Chain) Height(ctx context.Context) (int64, error) {
	return c.GetNode().Height(ctx)
}

func (c *Chain) Start(ctx context.Context) error {
	chainCfg := c.cfg

	genesisAmounts := make([][]sdk.Coin, len(c.Validators))
	genesisSelfDelegation := make([]sdk.Coin, len(c.Validators))

	for i := range c.Validators {
		genesisAmounts[i] = []sdk.Coin{{Amount: sdkmath.NewInt(10_000_000_000_000), Denom: chainCfg.Denom}}
		genesisSelfDelegation[i] = sdk.Coin{Amount: sdkmath.NewInt(5_000_000), Denom: chainCfg.Denom}
	}

	configFileOverrides := chainCfg.ConfigFileOverrides

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

	genbz = bytes.ReplaceAll(genbz, []byte(`"stake"`), []byte(fmt.Sprintf(`"%s"`, chainCfg.Denom)))

	if c.cfg.ModifyGenesis != nil {
		genbz, err = c.cfg.ModifyGenesis(chainCfg, genbz)
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

	peers := chainNodes.PeerString(ctx)

	eg, egCtx = errgroup.WithContext(ctx)
	for _, n := range chainNodes {
		c.log.Info("Starting container", zap.String("container", n.Name()))
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

func (tn *ChainNode) StartContainer(ctx context.Context) error {
	if err := tn.containerLifecycle.StartContainer(ctx); err != nil {
		return err
	}

	// Set the host ports once since they will not change after the container has started.
	hostPorts, err := tn.containerLifecycle.GetHostPorts(ctx, rpcPort, grpcPort, apiPort, p2pPort)
	if err != nil {
		return err
	}
	tn.hostRPCPort, tn.hostGRPCPort, tn.hostAPIPort, tn.hostP2PPort = hostPorts[0], hostPorts[1], hostPorts[2], hostPorts[3]

	err = tn.NewClient("tcp://" + tn.hostRPCPort)
	if err != nil {
		return err
	}

	time.Sleep(5 * time.Second)
	return retry.Do(func() error {
		stat, err := tn.Client.Status(ctx)
		if err != nil {
			return err
		}
		// TODO: re-enable this check, having trouble with it for some reason
		if stat != nil && stat.SyncInfo.CatchingUp {
			return fmt.Errorf("still catching up: height(%d) catching-up(%t)",
				stat.SyncInfo.LatestBlockHeight, stat.SyncInfo.CatchingUp)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(40), retry.Delay(3*time.Second), retry.DelayType(retry.FixedDelay))
}

// NewClient creates and assigns a new Tendermint RPC client to the ChainNode.
func (tn *ChainNode) NewClient(addr string) error {
	httpClient, err := libclient.DefaultHTTPClient(addr)
	if err != nil {
		return err
	}

	httpClient.Timeout = 10 * time.Second
	rpcClient, err := rpchttp.NewWithClient(addr, "/websocket", httpClient)
	if err != nil {
		return err
	}

	tn.Client = rpcClient

	grpcConn, err := grpc.NewClient(
		tn.hostGRPCPort, grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("grpc dial: %w", err)
	}
	tn.GrpcConn = grpcConn

	return nil
}

// SetPeers modifies the config persistent_peers for a node.
func (tn *ChainNode) SetPeers(ctx context.Context, peers string) error {
	c := make(toml.Toml)
	p2p := make(toml.Toml)

	// Set peers
	p2p["persistent_peers"] = peers
	c["p2p"] = p2p

	return ModifyConfigFile(
		ctx,
		tn.logger(),
		tn.DockerClient,
		tn.TestName,
		tn.VolumeName,
		"config/config.toml",
		c,
	)
}

func (tn *ChainNode) CreateNodeContainer(ctx context.Context) error {
	chainCfg := tn.cfg

	var cmd []string
	if chainCfg.NoHostMount {
		startCmd := fmt.Sprintf("cp -r %s %s_nomnt && %s start --home %s_nomnt", tn.HomeDir(), tn.HomeDir(), chainCfg.Bin, tn.HomeDir())
		if len(chainCfg.AdditionalStartArgs) > 0 {
			startCmd = fmt.Sprintf("%s %s", startCmd, chainCfg.AdditionalStartArgs)
		}
		cmd = []string{"sh", "-c", startCmd}
	} else {
		cmd = []string{chainCfg.Bin, "start", "--home", tn.HomeDir()}
		if len(chainCfg.AdditionalStartArgs) > 0 {
			cmd = append(cmd, chainCfg.AdditionalStartArgs...)
		}
	}

	usingPorts := nat.PortMap{}
	for k, v := range sentryPorts {
		usingPorts[k] = v
	}
	for _, port := range chainCfg.ExposeAdditionalPorts {
		usingPorts[nat.Port(port)] = []nat.PortBinding{}
	}

	// to prevent port binding conflicts, host port overrides are only exposed on the first validator node.
	if tn.Validator && tn.Index == 0 && chainCfg.HostPortOverride != nil {
		var fields []zap.Field

		i := 0
		for intP, extP := range chainCfg.HostPortOverride {
			port := nat.Port(fmt.Sprintf("%d/tcp", intP))

			usingPorts[port] = []nat.PortBinding{
				{
					HostPort: fmt.Sprintf("%d", extP),
				},
			}

			fields = append(fields, zap.String(fmt.Sprintf("port_overrides_%d", i), fmt.Sprintf("%s:%d", port, extP)))
			i++
		}

		tn.log.Info("Port overrides", fields...)
	}

	return tn.containerLifecycle.CreateContainer(ctx, tn.TestName, tn.NetworkID, tn.Image, usingPorts, "", tn.Bind(), nil, tn.HostName(), cmd, chainCfg.Env, []string{})
}

func (tn *ChainNode) OverwriteGenesisFile(ctx context.Context, content []byte) error {
	err := tn.WriteFile(ctx, content, "config/genesis.json")
	if err != nil {
		return fmt.Errorf("overwriting genesis.json: %w", err)
	}

	return nil
}

// Nodes returns all nodes, including validators and fullnodes.
func (c *Chain) Nodes() ChainNodes {
	return append(c.Validators, c.FullNodes...)
}

// CollectGentxs runs collect gentxs on the node's home folders.
func (tn *ChainNode) CollectGentxs(ctx context.Context) error {
	tn.lock.Lock()
	defer tn.lock.Unlock()

	command := []string{tn.cfg.Bin, "genesis", "collect-gentxs", "--home", tn.HomeDir()}

	_, _, err := tn.Exec(ctx, command, tn.cfg.Env)
	return err
}

// ReadFile reads the contents of a single file at the specified path in the docker filesystem.
// relPath describes the location of the file in the docker volume relative to the home directory.
func (tn *ChainNode) ReadFile(ctx context.Context, relPath string) ([]byte, error) {
	fr := NewFileRetriever(tn.logger(), tn.DockerClient, tn.TestName)
	gen, err := fr.SingleFileContent(ctx, tn.VolumeName, relPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file at %s: %w", relPath, err)
	}
	return gen, nil
}

// WriteFile accepts file contents in a byte slice and writes the contents to
// the docker filesystem. relPath describes the location of the file in the
// docker volume relative to the home directory.
func (tn *ChainNode) WriteFile(ctx context.Context, content []byte, relPath string) error {
	fw := NewFileWriter(tn.logger(), tn.DockerClient, tn.TestName)
	return fw.WriteFile(ctx, tn.VolumeName, relPath, content)
}

func (tn *ChainNode) GenesisFileContent(ctx context.Context) ([]byte, error) {
	gen, err := tn.ReadFile(ctx, "config/genesis.json")
	if err != nil {
		return nil, fmt.Errorf("getting genesis.json content: %w", err)
	}

	return gen, nil
}

func (tn *ChainNode) copyGentx(ctx context.Context, destVal *ChainNode) error {
	nid, err := tn.NodeID(ctx)
	if err != nil {
		return fmt.Errorf("getting node ID: %w", err)
	}

	relPath := fmt.Sprintf("config/gentx/gentx-%s.json", nid)

	gentx, err := tn.ReadFile(ctx, relPath)
	if err != nil {
		return fmt.Errorf("getting gentx content: %w", err)
	}

	err = destVal.WriteFile(ctx, gentx, relPath)
	if err != nil {
		return fmt.Errorf("overwriting gentx: %w", err)
	}

	return nil
}

// NodeID returns the persistent ID of a given node.
func (tn *ChainNode) NodeID(ctx context.Context) (string, error) {
	// This used to call p2p.LoadNodeKey against the file on the host,
	// but because we are transitioning to operating on Docker volumes,
	// we only have to tmjson.Unmarshal the raw content.
	j, err := tn.ReadFile(ctx, "config/node_key.json")
	if err != nil {
		return "", fmt.Errorf("getting node_key.json content: %w", err)
	}

	var nk p2p.NodeKey
	if err := tmjson.Unmarshal(j, &nk); err != nil {
		return "", fmt.Errorf("unmarshaling node_key.json: %w", err)
	}

	return string(nk.ID()), nil
}

// AddGenesisAccount adds a genesis account for each key.
func (tn *ChainNode) AddGenesisAccount(ctx context.Context, address string, genesisAmount []sdk.Coin) error {
	tn.lock.Lock()
	defer tn.lock.Unlock()

	amount := ""
	for i, coin := range genesisAmount {
		if i != 0 {
			amount += ","
		}
		amount += fmt.Sprintf("%s%s", coin.Amount.String(), coin.Denom)
	}

	// Adding a genesis account should complete instantly,
	// so use a 1-minute timeout to more quickly detect if Docker has locked up.
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	command := []string{"genesis", "add-genesis-account", address, amount}
	_, _, err := tn.ExecBin(ctx, command...)

	return err
}

// InitValidatorGenTx creates the node files and signs a genesis transaction.
func (tn *ChainNode) InitValidatorGenTx(
	ctx context.Context,
	genesisAmounts []sdk.Coin,
	genesisSelfDelegation sdk.Coin,
) error {
	if err := tn.CreateKey(ctx, valKey); err != nil {
		return err
	}
	bech32, err := tn.AccountKeyBech32(ctx, valKey)
	if err != nil {
		return err
	}
	if err := tn.AddGenesisAccount(ctx, bech32, genesisAmounts); err != nil {
		return err
	}
	return tn.Gentx(ctx, valKey, genesisSelfDelegation)
}

// CreateKey creates a key in the keyring backend test for the given node.
func (tn *ChainNode) CreateKey(ctx context.Context, name string) error {
	tn.lock.Lock()
	defer tn.lock.Unlock()

	_, _, err := tn.ExecBin(ctx,
		"keys", "add", name,
		"--coin-type", tn.cfg.CoinType,
		"--keyring-backend", keyring.BackendTest,
	)
	return err
}

// Gentx generates the gentx for a given node.
func (tn *ChainNode) Gentx(ctx context.Context, name string, genesisSelfDelegation sdk.Coin) error {
	tn.lock.Lock()
	defer tn.lock.Unlock()

	command := []string{"genesis"}
	command = append(command, "gentx", name, fmt.Sprintf("%s%s", genesisSelfDelegation.Amount.String(), genesisSelfDelegation.Denom),
		"--gas-prices", tn.cfg.GasPrices,
		"--gas-adjustment", fmt.Sprint(tn.cfg.GasAdjustment),
		"--keyring-backend", keyring.BackendTest,
		"--chain-id", tn.cfg.ChainID,
	)

	_, _, err := tn.ExecBin(ctx, command...)
	return err
}

// AccountKeyBech32 retrieves the named key's address in bech32 account format.
func (tn *ChainNode) AccountKeyBech32(ctx context.Context, name string) (string, error) {
	return tn.KeyBech32(ctx, name, "")
}

// KeyBech32 retrieves the named key's address in bech32 format from the node.
// bech is the bech32 prefix (acc|val|cons). If empty, defaults to the account key (same as "acc").
func (tn *ChainNode) KeyBech32(ctx context.Context, name string, bech string) (string, error) {
	command := []string{
		tn.cfg.Bin, "keys", "show", "--address", name,
		"--home", tn.HomeDir(),
		"--keyring-backend", keyring.BackendTest,
	}

	if bech != "" {
		command = append(command, "--bech", bech)
	}

	stdout, stderr, err := tn.Exec(ctx, command, tn.cfg.Env)
	if err != nil {
		return "", fmt.Errorf("failed to show key %q (stderr=%q): %w", name, stderr, err)
	}

	return string(bytes.TrimSuffix(stdout, []byte("\n"))), nil
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
	image DockerImage,
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
