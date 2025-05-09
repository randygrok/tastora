package types

type Wallet struct {
	Address          []byte
	FormattedAddress string
	KeyName          string
}

func NewWallet(address []byte, formattedAddress string, keyName string) Wallet {
	return Wallet{
		Address:          address,
		FormattedAddress: formattedAddress,
		KeyName:          keyName,
	}
}
