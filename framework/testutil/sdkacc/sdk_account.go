package sdkacc

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"strings"
)

// AddressToBech32 converts an sdk address to a string, the prefix must be supplied.
func AddressToBech32(addr sdk.AccAddress, prefix string) (string, error) {
	return bech32.ConvertAndEncode(prefix, addr)
}

// AddressFromBech32 returns an sdk address from a string, the prefix must be supplied.
func AddressFromBech32(address, prefix string) (sdk.AccAddress, error) {
	if len(strings.TrimSpace(address)) == 0 {
		return sdk.AccAddress{}, fmt.Errorf("empty address string is not allowed")
	}

	bz, err := sdk.GetFromBech32(address, prefix)
	if err != nil {
		return nil, err
	}

	err = sdk.VerifyAddressFormat(bz)
	if err != nil {
		return nil, err
	}

	return bz, nil
}
