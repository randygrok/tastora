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

	broadcaster := NewBroadcaster(t, chain)

	fromAddr := sdk.AccAddress(chain.faucetWallet.FormattedAddress)
	toAddr := sdk.AccAddress(wallet.FormattedAddress)

	bankSend := banktypes.NewMsgSend(fromAddr, toAddr, sdk.NewCoins(sdk.NewCoin(chainCfg.Denom, amount)))
	resp, err := BroadcastTx(ctx, broadcaster, chain.faucetWallet, bankSend)
	if err != nil {
		return types.Wallet{}, fmt.Errorf("failed to get faucet user wallet: %w", err)
	}

	if resp.Code != 0 {
		return types.Wallet{}, fmt.Errorf("failed to broadcast bank send: %s", resp.RawLog)
	}

	return wallet, nil
}
