package circuits

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/gnark-crypto-primitives/elgamal"
	"github.com/vocdoni/gnark-crypto-primitives/emulated/bn254/twistededwards"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// BallotMode is a struct that contains the common inputs for all the voters.
// The values of this struct should be the same for all the voters in the same
// process. Is a generic struct that can be used with any type of circuit input.
type BallotMode[T any] struct {
	MaxCount        T
	ForceUniqueness T
	MaxValue        T
	MinValue        T
	MaxTotalCost    T
	MinTotalCost    T
	CostExp         T
	CostFromWeight  T
}

func (bm BallotMode[T]) Serialize() []T {
	return []T{
		bm.MaxCount,
		bm.ForceUniqueness,
		bm.MaxValue,
		bm.MinValue,
		bm.MaxTotalCost,
		bm.MinTotalCost,
		bm.CostExp,
		bm.CostFromWeight,
	}
}

// Bytes returns 8*32 bytes representing BallotMode components.
// Returns an empty slice if T is not *big.Int.
func (bm BallotMode[T]) Bytes() []byte {
	bmbi, ok := any(bm).(BallotMode[*big.Int])
	if !ok {
		return []byte{}
	}
	buf := bytes.Buffer{}
	for _, bigint := range bmbi.Serialize() {
		buf.Write(arbo.BigIntToBytes(crypto.SerializedFieldSize, bigint))
	}
	return buf.Bytes()
}

// BigIntsToEmulatedElementBN254 casts BallotMode[*big.Int] into a
// BallotMode[emulated.Element[sw_bn254.ScalarField]]
func (bm BallotMode[T]) BigIntsToEmulatedElementBN254() BallotMode[emulated.Element[sw_bn254.ScalarField]] {
	bmbi, ok := any(bm).(BallotMode[*big.Int])
	if !ok {
		return BallotMode[emulated.Element[sw_bn254.ScalarField]]{}
	}
	return BallotMode[emulated.Element[sw_bn254.ScalarField]]{
		MaxCount:        emulated.ValueOf[sw_bn254.ScalarField](bmbi.MaxCount),
		ForceUniqueness: emulated.ValueOf[sw_bn254.ScalarField](bmbi.ForceUniqueness),
		MaxValue:        emulated.ValueOf[sw_bn254.ScalarField](bmbi.MaxValue),
		MinValue:        emulated.ValueOf[sw_bn254.ScalarField](bmbi.MinValue),
		MaxTotalCost:    emulated.ValueOf[sw_bn254.ScalarField](bmbi.MaxTotalCost),
		MinTotalCost:    emulated.ValueOf[sw_bn254.ScalarField](bmbi.MinTotalCost),
		CostExp:         emulated.ValueOf[sw_bn254.ScalarField](bmbi.CostExp),
		CostFromWeight:  emulated.ValueOf[sw_bn254.ScalarField](bmbi.CostFromWeight),
	}
}

// VarsToEmulatedElementBN254 casts BallotMode[frontend.Variable] into a BallotMode[emulated.Element[sw_bn254.ScalarField]]
func (bm BallotMode[T]) VarsToEmulatedElementBN254(api frontend.API) BallotMode[emulated.Element[sw_bn254.ScalarField]] {
	bmv, ok := any(bm).(BallotMode[frontend.Variable])
	if !ok {
		return BallotMode[emulated.Element[sw_bn254.ScalarField]]{}
	}
	return BallotMode[emulated.Element[sw_bn254.ScalarField]]{
		MaxCount:        *varToEmulatedElementBN254(api, bmv.MaxCount),
		ForceUniqueness: *varToEmulatedElementBN254(api, bmv.ForceUniqueness),
		MaxValue:        *varToEmulatedElementBN254(api, bmv.MaxValue),
		MinValue:        *varToEmulatedElementBN254(api, bmv.MinValue),
		MaxTotalCost:    *varToEmulatedElementBN254(api, bmv.MaxTotalCost),
		MinTotalCost:    *varToEmulatedElementBN254(api, bmv.MinTotalCost),
		CostExp:         *varToEmulatedElementBN254(api, bmv.CostExp),
		CostFromWeight:  *varToEmulatedElementBN254(api, bmv.CostFromWeight),
	}
}

// DeserializeBallotMode reconstructs a BallotMode from a slice of bytes.
// The input must be of len 8*32 bytes (otherwise it returns an error),
// representing 8 big.Ints as little-endian.
func DeserializeBallotMode(data []byte) (BallotMode[*big.Int], error) {
	// Validate the input length
	expectedSize := 8 * crypto.SerializedFieldSize
	if len(data) != expectedSize {
		return BallotMode[*big.Int]{}, fmt.Errorf("invalid input length for BallotMode: got %d bytes, expected %d bytes", len(data), expectedSize)
	}
	// Helper function to extract *big.Int from a serialized slice
	readBigInt := func(offset int) *big.Int {
		return arbo.BytesToBigInt(data[offset : offset+crypto.SerializedFieldSize])
	}
	return BallotMode[*big.Int]{
		MaxCount:        readBigInt(0 * crypto.SerializedFieldSize),
		ForceUniqueness: readBigInt(1 * crypto.SerializedFieldSize),
		MaxValue:        readBigInt(2 * crypto.SerializedFieldSize),
		MinValue:        readBigInt(3 * crypto.SerializedFieldSize),
		MaxTotalCost:    readBigInt(4 * crypto.SerializedFieldSize),
		MinTotalCost:    readBigInt(5 * crypto.SerializedFieldSize),
		CostExp:         readBigInt(6 * crypto.SerializedFieldSize),
		CostFromWeight:  readBigInt(7 * crypto.SerializedFieldSize),
	}, nil
}

// BallotModeToCircuit converts a BallotMode to a circuit BallotMode which can
// be implemented with different base types.
func BallotModeToCircuit(b types.BallotMode) BallotMode[*big.Int] {
	return BallotMode[*big.Int]{
		MaxCount:        big.NewInt(int64(b.MaxCount)),
		ForceUniqueness: BoolToBigInt(b.ForceUniqueness),
		MaxValue:        b.MaxValue.MathBigInt(),
		MinValue:        b.MinValue.MathBigInt(),
		MaxTotalCost:    b.MaxTotalCost.MathBigInt(),
		MinTotalCost:    b.MinTotalCost.MathBigInt(),
		CostExp:         big.NewInt(int64(b.CostExponent)),
		CostFromWeight:  BoolToBigInt(b.CostFromWeight),
	}
}

type EncryptionKey[T any] struct {
	PubKey [2]T
}

func (k EncryptionKey[T]) Serialize() []T {
	return []T{k.PubKey[0], k.PubKey[1]}
}

// SerializeAsTE returns the EncryptionKey in Twisted Edwards format
func (kt EncryptionKey[T]) SerializeAsTE(api frontend.API) []emulated.Element[sw_bn254.ScalarField] {
	k, ok := any(kt).(EncryptionKey[emulated.Element[sw_bn254.ScalarField]])
	if !ok {
		panic("EncryptionKey type assertion failed")
	}
	kTE0, kTE1, err := twistededwards.FromEmulatedRTEtoTE(api, k.PubKey[0], k.PubKey[1])
	if err != nil {
		FrontendError(api, "failed to convert encryption key to RTE", err)
	}
	return []emulated.Element[sw_bn254.ScalarField]{kTE0, kTE1}
}

// Bytes returns 2*32 bytes representing PubKey components.
// Returns an empty slice if T is not *big.Int.
func (k EncryptionKey[T]) Bytes() []byte {
	ekbi, ok := any(k).(EncryptionKey[*big.Int])
	if !ok {
		return []byte{}
	}
	buf := bytes.Buffer{}
	for _, bigint := range ekbi.Serialize() {
		buf.Write(arbo.BigIntToBytes(crypto.SerializedFieldSize, bigint))
	}
	return buf.Bytes()
}

// BigIntsToEmulatedElementBN254 returns the EncryptionKey as a different type.
// Returns an empty EncryptionKey if T is not *big.Int.
func (k EncryptionKey[T]) BigIntsToEmulatedElementBN254() EncryptionKey[emulated.Element[sw_bn254.ScalarField]] {
	ekbi, ok := any(k).(EncryptionKey[*big.Int])
	if !ok {
		return EncryptionKey[emulated.Element[sw_bn254.ScalarField]]{}
	}
	return EncryptionKey[emulated.Element[sw_bn254.ScalarField]]{
		[2]emulated.Element[sw_bn254.ScalarField]{
			emulated.ValueOf[sw_bn254.ScalarField](ekbi.PubKey[0]),
			emulated.ValueOf[sw_bn254.ScalarField](ekbi.PubKey[1]),
		},
	}
}

// VarsToEmulatedElementBN254 returns the EncryptionKey as a different type.
// Returns an empty EncryptionKey if T is not frontend.Variable
func (k EncryptionKey[T]) VarsToEmulatedElementBN254(api frontend.API) EncryptionKey[emulated.Element[sw_bn254.ScalarField]] {
	ekv, ok := any(k).(EncryptionKey[frontend.Variable])
	if !ok {
		return EncryptionKey[emulated.Element[sw_bn254.ScalarField]]{}
	}
	return EncryptionKey[emulated.Element[sw_bn254.ScalarField]]{
		[2]emulated.Element[sw_bn254.ScalarField]{
			*varToEmulatedElementBN254(api, ekv.PubKey[0]),
			*varToEmulatedElementBN254(api, ekv.PubKey[1]),
		},
	}
}

// AsVar returns the EncryptionKey as a different type.
// Returns an empty EncryptionKey if T is not *big.Int.
func (k EncryptionKey[T]) AsVar() EncryptionKey[frontend.Variable] {
	ekbi, ok := any(k).(EncryptionKey[*big.Int])
	if !ok {
		return EncryptionKey[frontend.Variable]{}
	}
	return EncryptionKey[frontend.Variable]{
		[2]frontend.Variable{
			ekbi.PubKey[0],
			ekbi.PubKey[1],
		},
	}
}

// DeserializeEncryptionKey reconstructs a EncryptionKey from a slice of bytes.
// The input must be of len 2*32 bytes (otherwise it returns an error),
// representing 2 big.Ints as little-endian.
func DeserializeEncryptionKey(data []byte) (EncryptionKey[*big.Int], error) {
	// Validate the input length
	expectedSize := 2 * crypto.SerializedFieldSize
	if len(data) != expectedSize {
		return EncryptionKey[*big.Int]{}, fmt.Errorf("invalid input length for EncryptionKey: got %d bytes, expected %d bytes", len(data), expectedSize)
	}
	// Helper function to extract *big.Int from a serialized slice
	readBigInt := func(offset int) *big.Int {
		return arbo.BytesToBigInt(data[offset : offset+crypto.SerializedFieldSize])
	}
	return EncryptionKey[*big.Int]{
		PubKey: [2]*big.Int{
			readBigInt(0 * crypto.SerializedFieldSize),
			readBigInt(1 * crypto.SerializedFieldSize),
		},
	}, nil
}

func EncryptionKeyFromECCPoint(p ecc.Point) EncryptionKey[*big.Int] {
	ekX, ekY := p.Point()
	return EncryptionKey[*big.Int]{PubKey: [2]*big.Int{ekX, ekY}}
}

func EncryptionKeyToCircuit(k types.EncryptionKey) EncryptionKey[*big.Int] {
	return EncryptionKey[*big.Int]{PubKey: [2]*big.Int{k.X, k.Y}}
}

// Process is a struct that contains the common inputs for a process.
// Is a generic struct that can be used with any type of circuit input.
type Process[T any] struct {
	ID            T
	CensusRoot    T
	BallotMode    BallotMode[T]
	EncryptionKey EncryptionKey[T]
}

// Serialize returns a slice with the process parameters in order
//
//	Process.ID
//	Process.CensusRoot
//	Process.BallotMode
//	Process.EncryptionKey
func (p Process[T]) Serialize() []T {
	list := []T{}
	list = append(list, p.ID)
	list = append(list, p.CensusRoot)
	list = append(list, p.BallotMode.Serialize()...)
	list = append(list, p.EncryptionKey.Serialize()...)
	return list
}

// SerializeForBallotProof returns a slice with the process parameters in order
//
//	Process.ID
//	Process.BallotMode
//	Process.EncryptionKey (in Twisted Edwards format)
func (pt Process[T]) SerializeForBallotProof(api frontend.API) []emulated.Element[sw_bn254.ScalarField] {
	p, ok := any(pt).(Process[emulated.Element[sw_bn254.ScalarField]])
	if !ok {
		panic("Process type assertion failed")
	}
	list := []emulated.Element[sw_bn254.ScalarField]{}
	list = append(list, p.ID)
	list = append(list, p.BallotMode.Serialize()...)
	list = append(list, p.EncryptionKey.SerializeAsTE(api)...)
	return list
}

func (p Process[T]) VarsToEmulatedElementBN254(api frontend.API) Process[emulated.Element[sw_bn254.ScalarField]] {
	return Process[emulated.Element[sw_bn254.ScalarField]]{
		ID:            *varToEmulatedElementBN254(api, p.ID),
		CensusRoot:    *varToEmulatedElementBN254(api, p.CensusRoot),
		BallotMode:    p.BallotMode.VarsToEmulatedElementBN254(api),
		EncryptionKey: p.EncryptionKey.VarsToEmulatedElementBN254(api),
	}
}

// Vote is a struct that contains all data related to a vote.
// Is a generic struct that can be used with any type of circuit input.
type Vote[T any] struct {
	Nullifier  T
	Ballot     Ballot
	Address    T
	Commitment T
}

func (v Vote[T]) ToEmulatedVote(api frontend.API) EmulatedVote[sw_bn254.ScalarField] {
	return EmulatedVote[sw_bn254.ScalarField]{
		Nullifier:  *varToEmulatedElementBN254(api, v.Nullifier),
		Ballot:     v.Ballot.ToEmulatedBallot(api),
		Address:    *varToEmulatedElementBN254(api, v.Address),
		Commitment: *varToEmulatedElementBN254(api, v.Commitment),
	}
}

func (v Vote[T]) SerializeAsVars() []frontend.Variable {
	// enforce that T is frontend.Variable
	_, ok := any(v).(Vote[frontend.Variable])
	if !ok {
		panic("Vote type assertion failed")
	}
	list := []frontend.Variable{}
	list = append(list, v.Address)
	list = append(list, v.Commitment)
	list = append(list, v.Nullifier)
	list = append(list, v.Ballot.SerializeVars()...)
	return list
}

type Ballot [FieldsPerBallot]elgamal.Ciphertext

func NewBallot() *Ballot {
	z := &Ballot{}
	for i := range z {
		z[i] = *elgamal.NewCiphertext()
	}
	return z
}

// Add sets z to the sum x+y and returns z.
//
// Panics if twistededwards curve init fails.
func (z *Ballot) Add(api frontend.API, x, y *Ballot) *Ballot {
	for i := range z {
		z[i].Add(api, &x[i], &y[i])
	}
	return z
}

// AssertIsEqual fails if any of the fields differ between z and x
func (z *Ballot) AssertIsEqual(api frontend.API, x *Ballot) {
	for i := range z {
		z[i].AssertIsEqual(api, &x[i])
	}
}

// Select if b is true, sets z = i1, else z = i2, and returns z
func (z *Ballot) Select(api frontend.API, b frontend.Variable, i1 *Ballot, i2 *Ballot) *Ballot {
	for i := range z {
		z[i] = *z[i].Select(api, b, &i1[i], &i2[i])
	}
	return z
}

// Serialize returns a slice with the C1.X, C1.Y, C2.X, C2.Y in order
func (z *Ballot) Serialize(api frontend.API) []emulated.Element[sw_bn254.ScalarField] {
	vars := []emulated.Element[sw_bn254.ScalarField]{}
	for i := range z {
		for _, zi := range z[i].Serialize() {
			elem, err := utils.UnpackVarToScalar[sw_bn254.ScalarField](api, zi)
			if err != nil {
				panic(err)
			}
			vars = append(vars, *elem)
		}
	}
	return vars
}

// Serialize returns a slice with the C1.X, C1.Y, C2.X, C2.Y in order
func (z *Ballot) SerializeVars() []frontend.Variable {
	vars := []frontend.Variable{}
	for i := range z {
		vars = append(vars, z[i].Serialize()...)
	}
	return vars
}

func (z *Ballot) ToEmulatedBallot(api frontend.API) EmulatedBallot[sw_bn254.ScalarField] {
	ez := EmulatedBallot[sw_bn254.ScalarField]{}
	for i := range ez {
		ez[i].C1.X = *varToEmulatedElementBN254(api, z[i].C1.X)
		ez[i].C1.Y = *varToEmulatedElementBN254(api, z[i].C1.Y)
		ez[i].C2.X = *varToEmulatedElementBN254(api, z[i].C2.X)
		ez[i].C2.Y = *varToEmulatedElementBN254(api, z[i].C2.Y)
	}
	return ez
}

// EmulatedPoint struct is a copy of the elgamal.Point struct, but using the
// emulated.Element type
type EmulatedPoint[F emulated.FieldParams] struct {
	X, Y emulated.Element[F]
}

// EmulatedCiphertext struct is a copy of the elgamal.Ciphertext struct, but
// using the EmulatedPoint type
type EmulatedCiphertext[F emulated.FieldParams] struct {
	C1, C2 EmulatedPoint[F]
}

// EmulatedBallot is a copy of the Ballot struct, but using the
// EmulatedCiphertext type
type EmulatedBallot[F emulated.FieldParams] [FieldsPerBallot]EmulatedCiphertext[F]

// EmulatedVote is a copy of the Vote struct, but using the emulated.Element
// type as generic type for the Address, Commitment and Nullifier fields, and
// the EmulatedBallot type for the Ballot field.
type EmulatedVote[F emulated.FieldParams] struct {
	Address    emulated.Element[F]
	Commitment emulated.Element[F]
	Nullifier  emulated.Element[F]
	Ballot     EmulatedBallot[F]
}

// Serialize returns a slice with the vote parameters in order
//
//	EmulatedVote.Address
//	EmulatedVote.Commitment
//	EmulatedVote.Nullifier
//	EmulatedVote.Ballot
func (z *EmulatedVote[F]) Serialize() []emulated.Element[F] {
	list := []emulated.Element[F]{}
	list = append(list, z.Address)
	list = append(list, z.Commitment)
	list = append(list, z.Nullifier)
	list = append(list, z.Ballot.Serialize()...)
	return list
}

// SerializeForBallotProof returns a slice with the vote parameters in order
//
//	EmulatedVote.Address
//	EmulatedVote.Commitment
//	EmulatedVote.Nullifier
//	EmulatedVote.Ballot (in Twisted Edwards format)
func (zt *EmulatedVote[F]) SerializeForBallotProof(api frontend.API) []emulated.Element[sw_bn254.ScalarField] {
	z, ok := any(zt).(*EmulatedVote[sw_bn254.ScalarField])
	if !ok {
		panic("EmulatedVote type assertion failed")
	}
	list := []emulated.Element[sw_bn254.ScalarField]{}
	list = append(list, z.Address)
	list = append(list, z.Commitment)
	list = append(list, z.Nullifier)
	list = append(list, z.Ballot.SerializeAsTE(api)...)
	return list
}

// NewEmulatedBallot returns a new EmulatedBallot with all fields with both
// points to zero point (0, 1).
func NewEmulatedBallot[F emulated.FieldParams]() *EmulatedBallot[F] {
	field := EmulatedCiphertext[F]{
		C1: EmulatedPoint[F]{X: emulated.ValueOf[F](0), Y: emulated.ValueOf[F](1)},
		C2: EmulatedPoint[F]{X: emulated.ValueOf[F](0), Y: emulated.ValueOf[F](1)},
	}
	z := &EmulatedBallot[F]{}
	for i := range z {
		z[i] = field
	}
	return z
}

// Serialize returns a slice with the C1.X, C1.Y, C2.X, C2.Y in order
func (z *EmulatedBallot[F]) Serialize() []emulated.Element[F] {
	list := []emulated.Element[F]{}
	for _, zi := range z {
		list = append(list,
			zi.C1.X,
			zi.C1.Y,
			zi.C2.X,
			zi.C2.Y)
	}
	return list
}

// SerializeAsTE returns a slice with the C1.X, C1.Y, C2.X, C2.Y in order,
// in Twisted Edwards format (rather than Reduced Twisted Edwards)
func (zt *EmulatedBallot[F]) SerializeAsTE(api frontend.API) []emulated.Element[sw_bn254.ScalarField] {
	z, ok := any(zt).(*EmulatedBallot[sw_bn254.ScalarField])
	if !ok {
		panic("EmulatedBallot type assertion failed")
	}
	list := []emulated.Element[sw_bn254.ScalarField]{}
	for _, zi := range z {
		c1xTE, c1yTE, err := twistededwards.FromEmulatedRTEtoTE(api, zi.C1.X, zi.C1.Y)
		if err != nil {
			FrontendError(api, "failed to convert coords to RTE", err)
		}
		c2xTE, c2yTE, err := twistededwards.FromEmulatedRTEtoTE(api, zi.C2.X, zi.C2.Y)
		if err != nil {
			FrontendError(api, "failed to convert coords to RTE", err)
		}
		list = append(list,
			c1xTE,
			c1yTE,
			c2xTE,
			c2yTE,
		)
	}
	return list
}

func varToEmulatedElementBN254(api frontend.API, v frontend.Variable) *emulated.Element[sw_bn254.ScalarField] {
	elem, err := utils.UnpackVarToScalar[sw_bn254.ScalarField](api, v)
	if err != nil {
		panic(err)
	}
	return elem
}
