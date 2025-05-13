package types

type Wallet interface {
	GetKeyName() string
	GetFormattedAddress() string
}
