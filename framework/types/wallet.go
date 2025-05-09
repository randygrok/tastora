package types

type Wallet struct {
	Address          []byte
	FormattedAddress string
	KeyName          string
}

func (w *Wallet) GetKeyName() string {
	return w.KeyName
}

func (w *Wallet) GetFormattedAddress() string {
	return w.FormattedAddress
}

func NewWallet(address []byte, formattedAddress string, keyName string) Wallet {
	return Wallet{
		Address:          address,
		FormattedAddress: formattedAddress,
		KeyName:          keyName,
	}
}
