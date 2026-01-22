package testutil

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand/v2"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/iden3/go-iden3-crypto/babyjub"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/spec/params"
	spectestutil "github.com/vocdoni/davinci-node/spec/testutil"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
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

func FixedStateRoot() *types.BigInt {
	bi, ok := new(big.Int).SetString("6980206406614621291864198316968348419717519918519760483937482600927519745732", 10)
	if !ok {
		panic("bad const in TestStateRoot")
	}
	return (*types.BigInt)(bi)
}

func DeterministicStateRoot(n uint64) *types.BigInt {
	return (*types.BigInt)(new(big.Int).SetUint64(n))
}

func RandomStateRoot() *types.BigInt {
	return (*types.BigInt)(new(big.Int).SetBytes(util.RandomBytes(16)))
}

func DeterministicAddress(n uint64) common.Address {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)

	prefix := []byte("deterministic-address:")
	h := crypto.Keccak256(append(prefix, b[:]...))
	return common.BytesToAddress(h[12:])
}

func RandomAddress() common.Address {
	return DeterministicAddress(rand.Uint64())
}

func RandomVoteID() types.VoteID {
	k, err := specutil.RandomK()
	if err != nil {
		panic(err)
	}
	voteID, err := spec.VoteID(
		RandomProcessID().MathBigInt(),
		RandomAddress().Big(),
		k,
	)
	if err != nil {
		panic(err)
	}
	return types.VoteID(voteID)
}

func RandomVoteIDs(n int) []types.VoteID {
	s := make([]types.VoteID, 0, n)
	for range n {
		s = append(s, RandomVoteID())
	}
	return s
}

func RandomCensus(origin types.CensusOrigin) *types.Census {
	return &types.Census{
		CensusOrigin: origin,
		CensusRoot:   RandomCensusRoot().Bytes(),
		CensusURI:    "http://example.com/census",
	}
}

func RandomEncryptionKeys() (babyjub.PrivateKey, types.EncryptionKey) {
	privkey := babyjub.NewRandPrivKey()

	x, y := format.FromTEtoRTE(privkey.Public().X, privkey.Public().Y)
	ek := new(bjj.BJJ).SetPoint(x, y)
	encKey := types.EncryptionKeyFromPoint(ek)

	return privkey, encKey
}

func RandomEncryptionPubKey() types.EncryptionKey {
	_, encryptionKey := RandomEncryptionKeys()
	return encryptionKey
}

func RandomProcess(processID types.ProcessID) *types.Process {
	ek := RandomEncryptionPubKey()
	stateRoot, err := spec.StateRoot(processID.MathBigInt(),
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		ek.X.MathBigInt(), ek.Y.MathBigInt(), BallotModePacked())
	if err != nil {
		panic(fmt.Sprintf("stateroot: %v", err))
	}
	return &types.Process{
		ID:            &processID,
		Status:        types.ProcessStatusReady,
		StartTime:     time.Now(),
		Duration:      time.Hour,
		MetadataURI:   "http://example.com/metadata",
		EncryptionKey: &ek,
		StateRoot:     types.BigIntConverter(stateRoot),
		BallotMode:    BallotMode(),
		Census:        RandomCensus(types.CensusOriginMerkleTreeOffchainStaticV1),
	}
}

func BallotModePacked() *big.Int {
	packed, err := BallotMode().Pack()
	if err != nil {
		panic(err)
	}
	return packed
}

func BallotMode() spec.BallotMode {
	return spectestutil.FixedBallotMode()
}

// GenDeterministicBallotFields generates a list of n deterministic fields
// based on the provided seed for consistent testing and caching.
func GenDeterministicBallotFields(seed int64) [params.FieldsPerBallot]*types.BigInt {
	bm := spectestutil.FixedBallotMode()
	fields := [params.FieldsPerBallot]*types.BigInt{}
	for i := range len(fields) {
		fields[i] = types.NewInt(0)
	}

	// Use seed-based deterministic generation
	stored := map[string]bool{}
	for i := range bm.NumFields {
		for attempt := 0; ; attempt++ {
			// Generate deterministic field based on seed, index, and attempt
			fieldSeed := seed + int64(i)*1000 + int64(attempt)
			fieldValue := int64(bm.MinValue) + (fieldSeed % int64(bm.MaxValue-bm.MinValue))
			field := big.NewInt(fieldValue)

			// if it should be unique and it's already stored, try next attempt
			if bm.UniqueValues || !stored[field.String()] {
				fields[i] = fields[i].SetBigInt(field)
				stored[field.String()] = true
				break
			}
		}
	}
	return fields
}
