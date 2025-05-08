package circuits

import (
	"math"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/iden3/go-iden3-crypto/babyjub"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
)

const (
	// default process config
	MockMaxCount        = 5
	MockForceUniqueness = 0
	MockMaxValue        = 16
	MockMinValue        = 0
	MockCostExp         = 2
	MockCostFromWeight  = 0
	MockWeight          = 10
)

func MockEncryptionKey() EncryptionKey[*big.Int] {
	privkey := babyjub.NewRandPrivKey()

	x, y := privkey.Public().X, privkey.Public().Y
	return EncryptionKeyFromECCPoint(new(bjj.BJJ).SetPoint(x, y))
}

func MockBallotMode() BallotMode[*big.Int] {
	return BallotMode[*big.Int]{
		MaxCount:        big.NewInt(MockMaxCount),
		ForceUniqueness: big.NewInt(MockForceUniqueness),
		MaxValue:        big.NewInt(MockMaxValue),
		MinValue:        big.NewInt(MockMinValue),
		MaxTotalCost:    big.NewInt(int64(math.Pow(float64(MockMaxValue), float64(MockCostExp))) * MockMaxCount),
		MinTotalCost:    big.NewInt(MockMaxCount),
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
