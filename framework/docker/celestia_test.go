package docker

import (
	"context"
	"cosmossdk.io/math"
	"github.com/chatton/celestia-test/framework/testutil/maps"
	"github.com/chatton/celestia-test/framework/testutil/toml"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func appOverrides() toml.Toml {
	// required to query tx by hash when broadcasting transactions.
	appTomlOverride := make(toml.Toml)
	txIndexConfig := make(toml.Toml)
	txIndexConfig["indexer"] = "kv"
	appTomlOverride["tx-index"] = txIndexConfig
	return appTomlOverride
}

func configOverrides() toml.Toml {
	// required to query tx by hash when broadcasting transactions.
	overrides := make(toml.Toml)
	txIndexConfig := make(toml.Toml)
	txIndexConfig["indexer"] = "kv"
	overrides["tx_index"] = txIndexConfig
	return overrides
}

// TestCreateAndFundWallet tests deploying a Celestia chain and using the CreateAndFundTestWallet function.
func TestCreateAndFundWallet(t *testing.T) {
	sdkConf := sdk.GetConfig()
	sdkConf.SetBech32PrefixForAccount("celestia", "celestiapub")
	sdkConf.Seal()
	// Create a context
	ctx := context.Background()

	// Create a Docker client and network
	dockerClient, networkID := DockerSetup(t)
	defer DockerCleanup(t, dockerClient)()

	// Create a logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	enc := testutil.MakeTestEncodingConfig(auth.AppModuleBasic{}, bank.AppModuleBasic{})
	// Create provider configuration
	numValidators := 1
	numFullNodes := 0
	cfg := Config{
		Logger:          logger,
		DockerClient:    dockerClient,
		DockerNetworkID: networkID,
		ChainConfig: &ChainConfig{
			ConfigFileOverrides: map[string]any{
				// Use empty maps for config overrides
				"config/app.toml":    appOverrides(),
				"config/config.toml": configOverrides(),
			},
			Type:          "celestia",
			Name:          "celestia",
			Version:       "v0.23.0", // Use a fixed version
			NumValidators: &numValidators,
			NumFullNodes:  &numFullNodes,
			ChainID:       "celestia",
			Images: []DockerImage{
				{
					Repository: "ghcr.io/celestiaorg/celestia-app",
					Version:    "v4.0.0-rc4",
					UIDGID:     "10001:10001",
				},
			},
			Bin:           "celestia-appd",
			Bech32Prefix:  "celestia",
			Denom:         "utia",
			CoinType:      "118",
			GasPrices:     "0.025utia",
			GasAdjustment: 1.3,
			ModifyGenesis: func(config Config, bytes []byte) ([]byte, error) {
				return maps.SetField(bytes, "consensus.params.version.app", "4")
			},
			EncodingConfig:      &enc,
			AdditionalStartArgs: []string{"--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9098"},
		},
	}

	// Create provider
	provider := NewProvider(cfg, t)

	// Get chain
	chain, err := provider.GetChain(ctx)
	require.NoError(t, err)

	// Start the chain
	err = chain.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := chain.Stop(ctx)
		require.NoError(t, err)
	}()

	// Create and fund a test wallet
	amount := math.NewInt(1000000)
	wallet, err := CreateAndFundTestWallet(t, ctx, "test", amount, chain.(*Chain))
	require.NoError(t, err)
	require.NotNil(t, wallet)

	// Verify the wallet was created and funded correctly
	require.NotEmpty(t, wallet.GetFormattedAddress())
	t.Logf("Created and funded wallet with address: %s", wallet.GetFormattedAddress())
}
