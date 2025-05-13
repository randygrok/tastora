package docker

import "cosmossdk.io/math"

// TestCreateAndFundWallet tests wallet creation and funding.
func (s *DockerTestSuite) TestCreateAndFundWallet() {
	amount := math.NewInt(1000000)
	wallet, err := CreateAndFundTestWallet(s.T(), s.ctx, "test", amount, s.chain)
	s.Require().NoError(err)
	s.Require().NotNil(wallet)
	s.Require().NotEmpty(wallet.GetFormattedAddress())
	s.Require().NotEmpty(wallet.GetKeyName())
	s.T().Logf("Created and funded wallet with address: %s", wallet.GetFormattedAddress())
}
