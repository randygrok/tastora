package docker

import (
	"fmt"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	dockerclient "github.com/moby/moby/client"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"sync"
)

type ChainNodes []*ChainNode

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
	Image        Image
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

func NewDockerChainNode(log *zap.Logger, validator bool, cfg Config, testName string, image Image, index int) *ChainNode {
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
