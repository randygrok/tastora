package docker

import (
	"context"
	"fmt"
	"github.com/chatton/celestia-test/framework/testutil/toml"
	"github.com/chatton/celestia-test/framework/types"
	volumetypes "github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"
	"go.uber.org/zap"
)

var _ types.Node = &BridgeNode{}

const (
	bridgeNodeRPCPort = "26658/tcp"
	bridgeNodeP2PPort = "2121/tcp"
)

// bridgeNodePorts defines the default port mappings for the BridgeNode's RPC and P2P communication.
var bridgeNodePorts = nat.PortMap{
	nat.Port(bridgeNodeRPCPort): {},
	nat.Port(bridgeNodeP2PPort): {},
}

// newBridgeNode initializes and returns a new BridgeNode instance using the provided context, test name, and configuration.
func newBridgeNode(ctx context.Context, testName string, cfg Config) (types.Node, error) {
	if cfg.BridgeNodeConfig == nil {
		return nil, fmt.Errorf("bridge node config is nil")
	}

	image := cfg.BridgeNodeConfig.Images[0]

	bn := &BridgeNode{
		cfg: *cfg.BridgeNodeConfig,
		log: cfg.Logger.With(
			zap.String("node_type", "bridge"),
		),
		node: newNode(cfg.DockerNetworkID, cfg.DockerClient, testName, image, "/home/celestia"),
	}

	bn.containerLifecycle = NewContainerLifecycle(cfg.Logger, cfg.DockerClient, bn.Name())

	v, err := cfg.DockerClient.VolumeCreate(ctx, volumetypes.CreateOptions{
		Labels: map[string]string{
			CleanupLabel:   testName,
			NodeOwnerLabel: bn.Name(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating volume for chain node: %w", err)
	}
	bn.VolumeName = v.Name

	if err := SetVolumeOwner(ctx, VolumeOwnerOptions{
		Log:        bn.log,
		Client:     cfg.DockerClient,
		VolumeName: v.Name,
		ImageRef:   image.Ref(),
		TestName:   testName,
		UidGid:     image.UIDGID,
	}); err != nil {
		return nil, fmt.Errorf("set volume owner: %w", err)
	}

	return bn, nil
}

// BridgeNode is a docker implementation of a celestia bridge node.
type BridgeNode struct {
	*node
	cfg      BridgeNodeConfig
	log      *zap.Logger
	testName string
	// ports that are resolvable from the test runners themselves.
	hostRPCPort string
	hostP2PPort string
}

// Name of the test node container.
func (b *BridgeNode) Name() string {
	return fmt.Sprintf("%s-%s", b.GetType(), SanitizeContainerName(b.TestName))
}

// Start initializes and starts the BridgeNode with the provided core IP and genesis hash in the given context.
// It returns an error if the node initialization or startup fails.
func (b *BridgeNode) Start(ctx context.Context, coreIp string, genesisHash string) error {
	if err := b.initNode(ctx); err != nil {
		return fmt.Errorf("failed to initialize p2p network: %w", err)
	}

	if err := b.startBridgeNode(ctx, coreIp, genesisHash); err != nil {
		return fmt.Errorf("failed to start bridge node: %w", err)
	}

	return nil
}

// Stop gracefully stops and removes the container associated with the BridgeNode using the provided context.
func (b *BridgeNode) Stop(ctx context.Context) error {
	return b.containerLifecycle.RemoveContainer(ctx)
}

// GetType returns the node type as a string. For `BridgeNode`, it always returns "bridge".
func (b *BridgeNode) GetType() string {
	return "bridge"
}

// disableRPCAuth disables RPC authentication so that the tests can use the endpoints without configuring auth.
func (b *BridgeNode) disableRPCAuth(ctx context.Context) error {
	modifications := make(toml.Toml)
	rpc := make(toml.Toml)
	rpc["SkipAuth"] = true
	modifications["RPC"] = rpc

	return ModifyConfigFile(
		ctx,
		b.log,
		b.DockerClient,
		b.TestName,
		b.VolumeName,
		"config.toml",
		modifications,
	)
}

func (b *BridgeNode) startBridgeNode(ctx context.Context, coreIp, genesisHash string) error {
	env := []string{
		fmt.Sprintf("P2P_NETWORK=%s", b.cfg.ChainID),
		fmt.Sprintf("CELESTIA_CUSTOM=%s:%s", b.cfg.ChainID, genesisHash),
	}

	if err := b.CreateNodeContainer(ctx, coreIp, env); err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// TODO: eventually re-enable and test with auth.
	if err := b.disableRPCAuth(ctx); err != nil {
		return fmt.Errorf("failed to disable RPC auth: %w", err)
	}

	if err := b.containerLifecycle.StartContainer(ctx); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Set the host ports once since they will not change after the container has started.
	hostPorts, err := b.containerLifecycle.GetHostPorts(ctx, bridgeNodeRPCPort, bridgeNodeP2PPort)
	if err != nil {
		return err
	}

	b.hostRPCPort, b.hostP2PPort = hostPorts[0], hostPorts[1]
	return nil
}

func (b *BridgeNode) initNode(ctx context.Context) error {
	// note: my_celes_key is the default key name for the bridge node.
	cmd := []string{"celestia", "bridge", "init", "--p2p.network", b.cfg.ChainID, "--keyring.keyname", "my_celes_key", "--node.store", b.homeDir}
	_, _, err := b.Exec(ctx, b.log, cmd, nil)
	return err
}

func (b *BridgeNode) CreateNodeContainer(ctx context.Context, coreIp string, env []string) error {
	cmd := []string{"celestia", "bridge", "start", "--p2p.network", b.cfg.ChainID, "--core.ip", coreIp, "--rpc.addr", "0.0.0.0", "--rpc.port", "26658", "--keyring.keyname", "my_celes_key", "--node.store", b.homeDir}

	usingPorts := nat.PortMap{}
	for k, v := range bridgeNodePorts {
		usingPorts[k] = v
	}

	return b.containerLifecycle.CreateContainer(ctx, b.TestName, b.NetworkID, b.Image, usingPorts, "", b.Bind(), nil, b.HostName(), cmd, env, []string{})
}

// HostName of the test node container.
func (b *BridgeNode) HostName() string {
	return CondenseHostName(b.Name())
}
