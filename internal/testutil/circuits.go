package testutil

import (
	"math/big"
	"math/rand/v2"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/vocdoni/davinci-node/circuits"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

const (
	numFields      = 5
	uniqueValues   = 0
	maxValue       = 16
	minValue       = 0
	maxValueSum    = 1280 // (maxValue ^ costExponent) * numFields
	minValueSum    = numFields
	costExponent   = 2
	costFromWeight = 0
)

const (
	Weight = 42
)

// SepoliaChainID returns 11155111 (i.e. Sepolia)
func SepoliaChainID() uint32 {
	return 11155111
}

// DeterministicProcessID is a deterministic ProcessID used for testing purposes.
func DeterministicProcessID(n uint64) types.ProcessID {
	address := DeterministicAddress(n)
	return types.NewProcessID(
		address,
		types.ProcessIDVersion(SepoliaChainID(), address),
		n,
	)
}

// FixedProcessID should be used by all circuit tests to ensure consistent caching
// and proof reuse between tests.
func FixedProcessID() types.ProcessID {
	return DeterministicProcessID(1)
}

func RandomProcessID() types.ProcessID {
	return DeterministicProcessID(rand.Uint64())
}

func RandomCensusRoot() *big.Int {
	return new(big.Int).SetBytes(util.RandomBytes(16))
}

func StateRoot() *types.BigInt {
	bi, ok := new(big.Int).SetString("6980206406614621291864198316968348419717519918519760483937482600927519745732", 10)
	if !ok {
		panic("bad const in TestStateRoot")
	}
	return (*types.BigInt)(bi)
}

func RandomStateRoot() *types.BigInt {
	return (*types.BigInt)(new(big.Int).SetBytes(util.RandomBytes(16)))
}

func DeterministicAddress(n uint64) common.Address {
	if n < circuits.ReservedKeysOffset {
		n += circuits.ReservedKeysOffset
	}
	return common.BigToAddress(new(big.Int).SetUint64(n))
}

func RandomAddress() common.Address {
	return DeterministicAddress(rand.Uint64())
}

func RandomVoteID() *big.Int {
	k, err := circuits.RandK()
	if err != nil {
		panic(err)
	}
	voteID, err := circuits.VoteID(
		RandomProcessID(),
		RandomAddress(),
		new(types.BigInt).SetBigInt(k))
	if err != nil {
		panic(err)
	}
	return voteID.MathBigInt()
}

func RandomCensus(origin types.CensusOrigin) *types.Census {
	return &types.Census{
		CensusOrigin: origin,
		CensusRoot:   RandomCensusRoot().Bytes(),
		CensusURI:    "http://example.com/census",
	}
}

func RandomEncryptionKeys() (babyjub.PrivateKey, circuits.EncryptionKey[*big.Int]) {
	privkey := babyjub.NewRandPrivKey()

	x, y := format.FromTEtoRTE(privkey.Public().X, privkey.Public().Y)
	ek := new(bjj.BJJ).SetPoint(x, y)
	encKey := circuits.EncryptionKeyFromECCPoint(ek)

	return privkey, encKey
}

func RandomEncryptionPubKey() circuits.EncryptionKey[*big.Int] {
	_, encryptionKey := RandomEncryptionKeys()
	return encryptionKey
}

func BallotMode() circuits.BallotMode[*big.Int] {
	return circuits.BallotMode[*big.Int]{
		NumFields:      big.NewInt(numFields),
		UniqueValues:   big.NewInt(uniqueValues),
		MaxValue:       big.NewInt(maxValue),
		MinValue:       big.NewInt(minValue),
		MaxValueSum:    big.NewInt(maxValueSum),
		MinValueSum:    big.NewInt(minValueSum),
		CostExponent:   big.NewInt(costExponent),
		CostFromWeight: big.NewInt(costFromWeight),
	}
}

func BallotModeVar() circuits.BallotMode[frontend.Variable] {
	return circuits.BallotMode[frontend.Variable]{
		NumFields:      numFields,
		UniqueValues:   uniqueValues,
		MaxValue:       maxValue,
		MinValue:       minValue,
		MaxValueSum:    maxValueSum,
		MinValueSum:    minValueSum,
		CostExponent:   costExponent,
		CostFromWeight: costFromWeight,
	}
}

func BallotModeEmulated() circuits.BallotMode[emulated.Element[sw_bn254.ScalarField]] {
	return circuits.BallotMode[emulated.Element[sw_bn254.ScalarField]]{
		NumFields:      emulated.ValueOf[sw_bn254.ScalarField](numFields),
		UniqueValues:   emulated.ValueOf[sw_bn254.ScalarField](uniqueValues),
		MaxValue:       emulated.ValueOf[sw_bn254.ScalarField](maxValue),
		MinValue:       emulated.ValueOf[sw_bn254.ScalarField](minValue),
		MaxValueSum:    emulated.ValueOf[sw_bn254.ScalarField](maxValueSum),
		MinValueSum:    emulated.ValueOf[sw_bn254.ScalarField](minValueSum),
		CostExponent:   emulated.ValueOf[sw_bn254.ScalarField](costExponent),
		CostFromWeight: emulated.ValueOf[sw_bn254.ScalarField](costFromWeight),
	}
}

func BallotModeInternal() *types.BallotMode {
	return &types.BallotMode{
		NumFields:      numFields,
		UniqueValues:   uniqueValues == 1,
		MaxValue:       new(types.BigInt).SetInt(maxValue),
		MinValue:       new(types.BigInt).SetInt(minValue),
		MaxValueSum:    new(types.BigInt).SetInt(maxValueSum),
		MinValueSum:    new(types.BigInt).SetInt(minValueSum),
		CostExponent:   costExponent,
		CostFromWeight: costFromWeight == 1,
	}
}

// GenDeterministicBallotFields generates a list of n deterministic fields
// based on the provided seed for consistent testing and caching.
func GenDeterministicBallotFields(seed int64) [types.FieldsPerBallot]*types.BigInt {
	fields := [types.FieldsPerBallot]*types.BigInt{}
	for i := range len(fields) {
		fields[i] = types.NewInt(0)
	}

	// Use seed-based deterministic generation
	stored := map[string]bool{}
	for i := range numFields {
		for attempt := 0; ; attempt++ {
			// Generate deterministic field based on seed, index, and attempt
			fieldSeed := seed + int64(i*1000) + int64(attempt)
			fieldValue := int64(minValue) + (fieldSeed % int64(maxValue-minValue))
			field := big.NewInt(fieldValue)

			// if it should be unique and it's already stored, try next attempt
			if uniqueValues != 0 || !stored[field.String()] {
				fields[i] = fields[i].SetBigInt(field)
				stored[field.String()] = true
				break
			}
		}
	}
	return fields
}
