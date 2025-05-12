package docker

import (
	"context"
	"cosmossdk.io/math"
	"fmt"
	"github.com/chatton/celestia-test/framework/testutil/random"
	"github.com/chatton/celestia-test/framework/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"testing"
)

func GetAndFundTestWallet(
	t *testing.T,
	ctx context.Context,
	keyNamePrefix string,
	amount math.Int,
	chain *Chain,
) (types.Wallet, error) {
	t.Helper()
	chainCfg := chain.cfg.ChainConfig
	keyName := fmt.Sprintf("%s-%s-%s", keyNamePrefix, chainCfg.ChainID, random.LowerCaseLetterString(3))
	wallet, err := chain.CreateWallet(ctx, keyName)
	if err != nil {
		return types.Wallet{}, fmt.Errorf("failed to get source user wallet: %w", err)
	}

	fromAddr, err := AccAddressFromBech32(chain.faucetWallet.FormattedAddress, chainCfg.Bech32Prefix)
	if err != nil {
		return types.Wallet{}, fmt.Errorf("invalid from address: %w", err)
	}

	toAddr, err := AccAddressFromBech32(wallet.FormattedAddress, chainCfg.Bech32Prefix)
	if err != nil {
		return types.Wallet{}, fmt.Errorf("invalid to address: %w", err)
	}

	broadcaster := NewBroadcaster(t, chain)
	bankSend := banktypes.NewMsgSend(fromAddr, toAddr, sdk.NewCoins(sdk.NewCoin(chainCfg.Denom, amount)))
	resp, err := BroadcastTx(ctx, broadcaster, &chain.faucetWallet, bankSend)
	if err != nil {
		return types.Wallet{}, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	if resp.Code != 0 {
		return types.Wallet{}, fmt.Errorf("error in bank send response: %s", resp.RawLog)
	}

	return wallet, nil
}
