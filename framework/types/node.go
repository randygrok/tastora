package types

// NodeType represents any node type that can be converted to a string
type NodeType interface {
	String() string
}

// Consensus Node Types (Celestia blockchain consensus)

// ConsensusNodeType represents nodes that participate in Celestia consensus
type ConsensusNodeType int

const (
	// Celestia Chain Node Types (CometBFT consensus)
	NodeTypeValidator     ConsensusNodeType = iota // Validator node in blockchain
	NodeTypeConsensusFull                          // Full node in blockchain
)

// String returns the string representation of the ConsensusNodeType
func (n ConsensusNodeType) String() string {
	return consensusNodeTypeStrings[n]
}

var consensusNodeTypeStrings = [...]string{
	"validator",      // NodeTypeValidator
	"consensus-full", // NodeTypeConsensusFull
}

// Generic Node Types (Infrastructure and alternative systems)

// GenericNodeType represents other types of nodes (infrastructure, alternative consensus, etc.)
type GenericNodeType int

const (
	// Alternative Consensus Engines
	NodeTypeRollkit GenericNodeType = iota // Rollkit node (alternative consensus engine)

	// Infrastructure Services
	NodeTypeHermes // Hermes IBC relayer service
)

// String returns the string representation of the GenericNodeType
func (n GenericNodeType) String() string {
	return genericNodeTypeStrings[n]
}

var genericNodeTypeStrings = [...]string{
	"rollkit",        // NodeTypeRollkit
	"hermes-relayer", // NodeTypeHermes
}

// DA Node Types (Data Availability layer)

// DANodeType represents Data Availability layer node types
type DANodeType int

const (
	BridgeNode DANodeType = iota // Bridge node in DA network
	LightNode                    // Light node in DA network
	FullNode                     // Full node in DA network
)

// String returns the string representation of the DANodeType
func (n DANodeType) String() string {
	return daNodeTypeStrings[n]
}

var daNodeTypeStrings = [...]string{
	"bridge", // BridgeNode
	"light",  // LightNode
	"full",   // FullNode
}

// Interface Compliance Checks
var (
	_ NodeType = ConsensusNodeType(0)
	_ NodeType = GenericNodeType(0)
	_ NodeType = DANodeType(0)
)
