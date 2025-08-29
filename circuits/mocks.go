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
	MockNumFields      = 5
	MockUniqueValues   = 0
	MockMaxValue       = 16
	MockMinValue       = 0
	MockMaxValueSum    = 1280 // (MockMaxValue ^ MockCostExponent) * MockNumFields
	MockMinValueSum    = 5    // MockNumFields
	MockCostExponent   = 2
	MockCostFromWeight = 0
	MockWeight         = 10
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
		NumFields:      big.NewInt(MockNumFields),
		UniqueValues:   big.NewInt(MockUniqueValues),
		MaxValue:       big.NewInt(MockMaxValue),
		MinValue:       big.NewInt(MockMinValue),
		MaxValueSum:    big.NewInt(MockMaxValueSum),
		MinValueSum:    big.NewInt(MockMinValueSum),
		CostExponent:   big.NewInt(MockCostExponent),
		CostFromWeight: big.NewInt(MockCostFromWeight),
	}
}

func MockBallotModeVar() BallotMode[frontend.Variable] {
	return BallotMode[frontend.Variable]{
		NumFields:      MockNumFields,
		UniqueValues:   MockUniqueValues,
		MaxValue:       MockMaxValue,
		MinValue:       MockMinValue,
		MaxValueSum:    int(math.Pow(float64(MockMaxValue), float64(MockCostExponent))) * MockNumFields,
		MinValueSum:    MockNumFields,
		CostExponent:   MockCostExponent,
		CostFromWeight: MockCostFromWeight,
	}
}

func MockBallotModeEmulated() BallotMode[emulated.Element[sw_bn254.ScalarField]] {
	return BallotMode[emulated.Element[sw_bn254.ScalarField]]{
		NumFields:      emulated.ValueOf[sw_bn254.ScalarField](MockNumFields),
		UniqueValues:   emulated.ValueOf[sw_bn254.ScalarField](MockUniqueValues),
		MaxValue:       emulated.ValueOf[sw_bn254.ScalarField](MockMaxValue),
		MinValue:       emulated.ValueOf[sw_bn254.ScalarField](MockMinValue),
		MaxValueSum:    emulated.ValueOf[sw_bn254.ScalarField](int(math.Pow(float64(MockMaxValue), float64(MockCostExponent))) * MockNumFields),
		MinValueSum:    emulated.ValueOf[sw_bn254.ScalarField](MockNumFields),
		CostExponent:   emulated.ValueOf[sw_bn254.ScalarField](MockCostExponent),
		CostFromWeight: emulated.ValueOf[sw_bn254.ScalarField](MockCostFromWeight),
	}
}

func MockBallotModeInternal() *types.BallotMode {
	return &types.BallotMode{
		NumFields:      MockNumFields,
		UniqueValues:   MockUniqueValues == 1,
		MaxValue:       new(types.BigInt).SetInt(MockMaxValue),
		MinValue:       new(types.BigInt).SetInt(MockMinValue),
		MaxValueSum:    new(types.BigInt).SetInt(MockMaxValueSum),
		MinValueSum:    new(types.BigInt).SetInt(MockMinValueSum),
		CostExponent:   MockCostExponent,
		CostFromWeight: MockCostFromWeight == 1,
	}
}
