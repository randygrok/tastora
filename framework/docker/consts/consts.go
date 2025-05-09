package consts

const (
	CelestiaDockerPrefix = "celestia-test"
	// CleanupLabel is a docker label key targeted by DockerSetup when it cleans up docker resources.
	CleanupLabel = "celestia-test"
	// LabelPrefix is the reverse DNS format "namespace" for celestia test Docker labels.
	LabelPrefix = "org.celestia.celestia-test."
	// NodeOwnerLabel indicates the logical node owning a particular object (probably a volume).
	NodeOwnerLabel = LabelPrefix + "node-owner"
	// UserRootString defines the default root user and group identifier in the format "UID:GID".
	UserRootString = "0:0"
)
