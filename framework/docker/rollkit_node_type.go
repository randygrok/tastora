package docker

// RollkitNodeType represents a Rollkit node
type RollkitNodeType struct{}

// String returns the string representation of the RollkitNodeType
func (r RollkitNodeType) String() string {
	return "rollkit"
}

// RollkitType is the singleton instance representing a Rollkit node
var RollkitType = RollkitNodeType{}

// Interface Compliance Check - ensure RollkitNodeType implements the NodeType interface
var _ interface{ String() string } = RollkitType
