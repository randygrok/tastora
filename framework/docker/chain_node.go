package docker

import (
	"context"
	"fmt"
	"github.com/chatton/celestia-test/framework/testutil/toml"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	dockerclient "github.com/moby/moby/client"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"hash/fnv"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	valKey      = "validator"
	blockTime   = 2 // seconds
	p2pPort     = "26656/tcp"
	rpcPort     = "26657/tcp"
	grpcPort    = "9090/tcp"
	apiPort     = "1317/tcp"
	privValPort = "1234/tcp"
)

type ChainNodes []*ChainNode

// PeerString returns the string for connecting the nodes passed in.
func (nodes ChainNodes) PeerString(ctx context.Context) string {
	return nodes.hostString(ctx, "Peer", 26656)
}

// RPCString returns the string for connecting the nodes passed in.
func (nodes ChainNodes) RPCString(ctx context.Context) string {
	return nodes.hostString(ctx, "RPC", 26657)
}

// hostString generates a comma-separated string of node addresses in the format "id@hostname:port" for the given nodes.
func (nodes ChainNodes) hostString(ctx context.Context, msg string, port int) string {
	addrs := make([]string, len(nodes))
	for i, n := range nodes {
		id, err := n.NodeID(ctx)
		if err != nil {
			// TODO: would this be better to panic?
			// When would NodeId return an error?
			break
		}
		hostName := n.HostName()
		addr := fmt.Sprintf("%s@%s:%d", id, hostName, port)
		n.cfg.Logger.Info(msg,
			zap.String("host_name", hostName),
			zap.String("address", addr),
			zap.String("container", n.Name()),
		)
		addrs[i] = addr
	}
	return strings.Join(addrs, ",")
}

type ChainNode struct {
	VolumeName   string
	Index        int
	cfg          Config
	Validator    bool
	NetworkID    string
	DockerClient *dockerclient.Client
	Client       rpcclient.Client
	GrpcConn     *grpc.ClientConn
	TestName     string
	Image        DockerImage
	preStartNode func(*ChainNode)

	lock sync.Mutex
	log  *zap.Logger

	containerLifecycle *ContainerLifecycle

	// Ports set during StartContainer.
	hostRPCPort   string
	hostAPIPort   string
	hostGRPCPort  string
	hostP2PPort   string
	cometHostname string
}

func NewDockerChainNode(log *zap.Logger, validator bool, cfg Config, testName string, image DockerImage, index int) *ChainNode {
	tn := &ChainNode{
		log: log.With(
			zap.Bool("validator", validator),
			zap.Int("i", index),
		),
		Validator:    validator,
		cfg:          cfg,
		DockerClient: cfg.DockerClient,
		NetworkID:    cfg.DockerNetworkID,
		TestName:     testName,
		Image:        image,
		Index:        index,
	}

	tn.containerLifecycle = NewContainerLifecycle(log, cfg.DockerClient, tn.Name())

	return tn
}

// HostName of the test node container.
func (tn *ChainNode) HostName() string {
	return CondenseHostName(tn.Name())
}

// Name of the test node container.
func (tn *ChainNode) Name() string {
	return fmt.Sprintf("%s-%s-%d-%s", tn.cfg.ChainID, tn.NodeType(), tn.Index, SanitizeContainerName(tn.TestName))
}

func (tn *ChainNode) NodeType() string {
	nodeType := "fn"
	if tn.Validator {
		nodeType = "val"
	}
	return nodeType
}

func (tn *ChainNode) Height(ctx context.Context) (int64, error) {
	res, err := tn.Client.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("tendermint rpc client status: %w", err)
	}
	height := res.SyncInfo.LatestBlockHeight
	return height, nil
}

func (tn *ChainNode) InitFullNodeFiles(ctx context.Context) error {
	if err := tn.InitHomeFolder(ctx); err != nil {
		return err
	}

	return tn.SetTestConfig(ctx)
}

// BinCommand is a helper to retrieve a full command for a chain node binary.
// For example, if chain node binary is `gaiad`, and desired command is `gaiad keys show key1`,
// pass ("keys", "show", "key1") for command to return the full command.
// Will include additional flags for home directory and chain ID.
func (tn *ChainNode) BinCommand(command ...string) []string {
	command = append([]string{tn.cfg.Bin}, command...)
	return append(command,
		"--home", tn.HomeDir(),
	)
}

func (tn *ChainNode) Exec(ctx context.Context, cmd []string, env []string) ([]byte, []byte, error) {
	job := NewImage(tn.logger(), tn.DockerClient, tn.NetworkID, tn.TestName, tn.Image.Repository, tn.Image.Version)
	opts := ContainerOptions{
		Env:   env,
		Binds: tn.Bind(),
	}
	res := job.Run(ctx, cmd, opts)
	return res.Stdout, res.Stderr, res.Err
}

// Bind returns the home folder bind point for running the node.
func (tn *ChainNode) Bind() []string {
	return []string{fmt.Sprintf("%s:%s", tn.VolumeName, tn.HomeDir())}
}

// ExecBin is a helper to execute a command for a chain node binary.
// For example, if chain node binary is `gaiad`, and desired command is `gaiad keys show key1`,
// pass ("keys", "show", "key1") for command to execute the command against the node.
// Will include additional flags for home directory and chain ID.
func (tn *ChainNode) ExecBin(ctx context.Context, command ...string) ([]byte, []byte, error) {
	return tn.Exec(ctx, tn.BinCommand(command...), tn.cfg.Env)
}

// InitHomeFolder initializes a home folder for the given node.
func (tn *ChainNode) InitHomeFolder(ctx context.Context) error {
	tn.lock.Lock()
	defer tn.lock.Unlock()

	_, _, err := tn.ExecBin(ctx,
		"init", CondenseMoniker(tn.Name()),
		"--chain-id", tn.cfg.ChainID,
	)
	return err
}

// SetTestConfig modifies the config to reasonable values for use within interchaintest.
func (tn *ChainNode) SetTestConfig(ctx context.Context) error {
	c := make(toml.Toml)

	// Set Log Level to info
	c["log_level"] = "info"

	p2p := make(toml.Toml)

	// Allow p2p strangeness
	p2p["allow_duplicate_ip"] = true
	p2p["addr_book_strict"] = false

	c["p2p"] = p2p

	consensus := make(toml.Toml)

	blockT := (time.Duration(blockTime) * time.Second).String()
	consensus["timeout_commit"] = blockT
	consensus["timeout_propose"] = blockT

	c["consensus"] = consensus

	rpc := make(toml.Toml)

	// Enable public RPC
	rpc["laddr"] = "tcp://0.0.0.0:26657"
	rpc["allowed_origins"] = []string{"*"}
	c["rpc"] = rpc

	if err := ModifyConfigFile(
		ctx,
		tn.logger(),
		tn.DockerClient,
		tn.TestName,
		tn.VolumeName,
		"config/config.toml",
		c,
	); err != nil {
		return err
	}

	a := make(toml.Toml)
	a["minimum-gas-prices"] = tn.cfg.GasPrices

	grpc := make(toml.Toml)

	// Enable public GRPC
	grpc["address"] = "0.0.0.0:9090"

	a["grpc"] = grpc

	api := make(toml.Toml)

	// Enable public REST API
	api["enable"] = true
	api["swagger"] = true
	api["address"] = "tcp://0.0.0.0:1317"

	a["api"] = api

	return ModifyConfigFile(
		ctx,
		tn.logger(),
		tn.DockerClient,
		tn.TestName,
		tn.VolumeName,
		"config/app.toml",
		a,
	)
}

func (tn *ChainNode) logger() *zap.Logger {
	return tn.cfg.Logger.With(
		zap.String("chain_id", tn.cfg.ChainID),
		zap.String("test", tn.TestName),
	)
}

func (tn *ChainNode) HomeDir() string {
	return path.Join("/var/cosmos-chain", tn.cfg.Name)
}

// CondenseMoniker fits a moniker into the cosmos character limit for monikers.
// If the moniker already fits, it is returned unmodified.
// Otherwise, the middle is truncated, and a hash is appended to the end
// in case the only unique data was in the middle.
func CondenseMoniker(m string) string {
	if len(m) <= stakingtypes.MaxMonikerLength {
		return m
	}

	// Get the hash suffix, a 32-bit uint formatted in base36.
	// fnv32 was chosen because a 32-bit number ought to be sufficient
	// as a distinguishing suffix, and it will be short enough so that
	// less of the middle will be truncated to fit in the character limit.
	// It's also non-cryptographic, not that this function will ever be a bottleneck in tests.
	h := fnv.New32()
	h.Write([]byte(m))
	suffix := "-" + strconv.FormatUint(uint64(h.Sum32()), 36)

	wantLen := stakingtypes.MaxMonikerLength - len(suffix)

	// Half of the want length, minus 2 to account for half of the ... we add in the middle.
	keepLen := (wantLen / 2) - 2

	return m[:keepLen] + "..." + m[len(m)-keepLen:] + suffix
}
