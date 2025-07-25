package circuits

import (
	"math"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/iden3/go-iden3-crypto/babyjub"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/types"
)

const (
	// default process config
	MockMaxCount        = 5
	MockForceUniqueness = 0
	MockMaxValue        = 16
	MockMinValue        = 0
	MockMaxTotalCost    = 1280 // (MockMaxValue ^ MockCostExp) * MockMaxCount
	MockMinTotalCost    = 5    // MockMaxCount
	MockCostExp         = 2
	MockCostFromWeight  = 0
	MockWeight          = 10
)

func MockEncryptionKey() (babyjub.PrivateKey, EncryptionKey[*big.Int]) {
	privkey := babyjub.NewRandPrivKey()

	x, y := format.FromTEtoRTE(privkey.Public().X, privkey.Public().Y)
	ek := new(bjj.BJJ).SetPoint(x, y)
	encKey := EncryptionKeyFromECCPoint(ek)

	return privkey, encKey
}

func MockBallotMode() BallotMode[*big.Int] {
	return BallotMode[*big.Int]{
		MaxCount:        big.NewInt(MockMaxCount),
		ForceUniqueness: big.NewInt(MockForceUniqueness),
		MaxValue:        big.NewInt(MockMaxValue),
		MinValue:        big.NewInt(MockMinValue),
		MaxTotalCost:    big.NewInt(MockMaxTotalCost),
		MinTotalCost:    big.NewInt(MockMinTotalCost),
		CostExp:         big.NewInt(MockCostExp),
		CostFromWeight:  big.NewInt(MockCostFromWeight),
	}
}

func MockBallotModeVar() BallotMode[frontend.Variable] {
	return BallotMode[frontend.Variable]{
		MaxCount:        MockMaxCount,
		ForceUniqueness: MockForceUniqueness,
		MaxValue:        MockMaxValue,
		MinValue:        MockMinValue,
		MaxTotalCost:    int(math.Pow(float64(MockMaxValue), float64(MockCostExp))) * MockMaxCount,
		MinTotalCost:    MockMaxCount,
		CostExp:         MockCostExp,
		CostFromWeight:  MockCostFromWeight,
	}
}

func MockBallotModeEmulated() BallotMode[emulated.Element[sw_bn254.ScalarField]] {
	return BallotMode[emulated.Element[sw_bn254.ScalarField]]{
		MaxCount:        emulated.ValueOf[sw_bn254.ScalarField](MockMaxCount),
		ForceUniqueness: emulated.ValueOf[sw_bn254.ScalarField](MockForceUniqueness),
		MaxValue:        emulated.ValueOf[sw_bn254.ScalarField](MockMaxValue),
		MinValue:        emulated.ValueOf[sw_bn254.ScalarField](MockMinValue),
		MaxTotalCost:    emulated.ValueOf[sw_bn254.ScalarField](int(math.Pow(float64(MockMaxValue), float64(MockCostExp))) * MockMaxCount),
		MinTotalCost:    emulated.ValueOf[sw_bn254.ScalarField](MockMaxCount),
		CostExp:         emulated.ValueOf[sw_bn254.ScalarField](MockCostExp),
		CostFromWeight:  emulated.ValueOf[sw_bn254.ScalarField](MockCostFromWeight),
	}
}

func MockBallotModeInternal() *types.BallotMode {
	return &types.BallotMode{
		MaxCount:        MockMaxCount,
		ForceUniqueness: MockForceUniqueness == 1,
		MaxValue:        new(types.BigInt).SetInt(MockMaxValue),
		MinValue:        new(types.BigInt).SetInt(MockMinValue),
		MaxTotalCost:    new(types.BigInt).SetInt(MockMaxTotalCost),
		MinTotalCost:    new(types.BigInt).SetInt(MockMinTotalCost),
		CostExponent:    MockCostExp,
		CostFromWeight:  MockCostFromWeight == 1,
	}
}
