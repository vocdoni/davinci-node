package circuits

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	tweds "github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/gnark-crypto-primitives/elgamal"
	emu_tweds "github.com/vocdoni/gnark-crypto-primitives/emulated/bn254/twistededwards"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/mimc7"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
)

const (
	BallotModeSerializedLen    = 8
	EncryptionKeySerializedLen = 2
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

func (bm BallotMode[T]) Deserialize(values []T) (BallotMode[T], error) {
	if len(values) != 8 {
		return BallotMode[T]{}, fmt.Errorf("invalid input length for BallotMode: expected 8 values")
	}
	return BallotMode[T]{
		MaxCount:        values[0],
		ForceUniqueness: values[1],
		MaxValue:        values[2],
		MinValue:        values[3],
		MaxTotalCost:    values[4],
		MinTotalCost:    values[5],
		CostExp:         values[6],
		CostFromWeight:  values[7],
	}, nil
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
		buf.Write(arbo.BigIntToBytes(crypto.SignatureCircuitVariableLen, bigint))
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
	expectedSize := 8 * crypto.SignatureCircuitVariableLen
	if len(data) != expectedSize {
		return BallotMode[*big.Int]{}, fmt.Errorf("invalid input length for BallotMode: got %d bytes, expected %d bytes", len(data), expectedSize)
	}
	// Helper function to extract *big.Int from a serialized slice
	readBigInt := func(offset int) *big.Int {
		return arbo.BytesToBigInt(data[offset : offset+crypto.SignatureCircuitVariableLen])
	}
	return BallotMode[*big.Int]{
		MaxCount:        readBigInt(0 * crypto.SignatureCircuitVariableLen),
		ForceUniqueness: readBigInt(1 * crypto.SignatureCircuitVariableLen),
		MaxValue:        readBigInt(2 * crypto.SignatureCircuitVariableLen),
		MinValue:        readBigInt(3 * crypto.SignatureCircuitVariableLen),
		MaxTotalCost:    readBigInt(4 * crypto.SignatureCircuitVariableLen),
		MinTotalCost:    readBigInt(5 * crypto.SignatureCircuitVariableLen),
		CostExp:         readBigInt(6 * crypto.SignatureCircuitVariableLen),
		CostFromWeight:  readBigInt(7 * crypto.SignatureCircuitVariableLen),
	}, nil
}

// BallotModeToCircuit converts a BallotMode to a circuit BallotMode which can
// be implemented with different base types.
// Before calling this function, the BallotMode must be validated.
func BallotModeToCircuit(b *types.BallotMode) BallotMode[*big.Int] {
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

func (k EncryptionKey[T]) Deserialize(values []T) (EncryptionKey[T], error) {
	if len(values) != 2 {
		return EncryptionKey[T]{}, fmt.Errorf("invalid input length for EncryptionKey: expected 2 values")
	}
	return EncryptionKey[T]{
		PubKey: [2]T{values[0], values[1]},
	}, nil
}

// SerializeAsTE returns the EncryptionKey in Twisted Edwards format
func (kt EncryptionKey[T]) SerializeAsTE(api frontend.API) []emulated.Element[sw_bn254.ScalarField] {
	k, ok := any(kt).(EncryptionKey[emulated.Element[sw_bn254.ScalarField]])
	if !ok {
		panic("EncryptionKey type assertion failed")
	}
	kTE0, kTE1, err := emu_tweds.FromEmulatedRTEtoTE(api, k.PubKey[0], k.PubKey[1])
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
		buf.Write(arbo.BigIntToBytes(crypto.SignatureCircuitVariableLen, bigint))
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
	expectedSize := 2 * crypto.SignatureCircuitVariableLen
	if len(data) != expectedSize {
		return EncryptionKey[*big.Int]{}, fmt.Errorf("invalid input length for EncryptionKey: got %d bytes, expected %d bytes", len(data), expectedSize)
	}
	// Helper function to extract *big.Int from a serialized slice
	readBigInt := func(offset int) *big.Int {
		return arbo.BytesToBigInt(data[offset : offset+crypto.SignatureCircuitVariableLen])
	}
	return EncryptionKey[*big.Int]{
		PubKey: [2]*big.Int{
			readBigInt(0 * crypto.SignatureCircuitVariableLen),
			readBigInt(1 * crypto.SignatureCircuitVariableLen),
		},
	}, nil
}

func EncryptionKeyFromECCPoint(p ecc.Point) EncryptionKey[*big.Int] {
	ekX, ekY := p.Point()
	return EncryptionKey[*big.Int]{PubKey: [2]*big.Int{ekX, ekY}}
}

func EncryptionKeyToCircuit(k types.EncryptionKey) EncryptionKey[*big.Int] {
	return EncryptionKey[*big.Int]{PubKey: [2]*big.Int{k.X.MathBigInt(), k.Y.MathBigInt()}}
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
	Ballot  Ballot
	VoteID  T
	Address T
}

func (v Vote[T]) ToEmulated(api frontend.API) EmulatedVote[sw_bn254.ScalarField] {
	return EmulatedVote[sw_bn254.ScalarField]{
		Ballot:  v.Ballot.ToEmulatedBallot(api),
		Address: *varToEmulatedElementBN254(api, v.Address),
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
	list = append(list, v.VoteID)
	list = append(list, v.Ballot.SerializeVars()...)
	return list
}

type Ballot [types.FieldsPerBallot]elgamal.Ciphertext

func NewBallot() *Ballot {
	z := &Ballot{}
	for i := range z {
		z[i] = *elgamal.NewCiphertext()
	}
	return z
}

// Encrypt encrypts the ballot using the provided encryption key and messages.
// It uses the MiMC hasher to generate a new k for each ciphertext starting
// from the provided k.
func (z *Ballot) Encrypt(
	api frontend.API,
	messages [types.FieldsPerBallot]frontend.Variable,
	encKey EncryptionKey[frontend.Variable],
	k frontend.Variable,
) *Ballot {
	// get the twisted edwards point from the encryption key
	pubKey := tweds.Point{
		X: encKey.PubKey[0],
		Y: encKey.PubKey[1],
	}
	for i := range z {
		// hash the last k to get a new one for the next ciphertext
		k = NextK(api, k)
		enc, err := z[i].Encrypt(api, pubKey, k, messages[i])
		if err != nil {
			FrontendError(api, "failed to encrypt ballot", err)
			return nil
		}
		z[i] = *enc
	}
	return z
}

// Reencrypt re-encrypts the ballot using the provided encryption key and the
// provided k. To re-encrypt the ballot, it uses the encrypted zero point with
// the inputs provided and them adds it to the original ballot. It uses the
// MiMC hasher to generate a new k for each ciphertext starting from the
// provided k.
func (z *Ballot) Reencrypt(api frontend.API, encKey EncryptionKey[frontend.Variable], k frontend.Variable) (*Ballot, frontend.Variable, error) {
	reencryptionK := NextK(api, k)
	encZero := NewBallot().EncryptedZero(api, encKey, reencryptionK)
	return NewBallot().Add(api, z, encZero), reencryptionK, nil
}

// AssertDecrypt checks that the ballot can be decrypted with the provided
// private key and the original values. It uses the elgamal.Ciphertext's
// AssertDecrypt method for each ciphertext in the ballot.
func (z *Ballot) AssertDecrypt(api frontend.API, privKey frontend.Variable, originals [types.FieldsPerBallot]frontend.Variable) {
	for i := range z {
		if err := z[i].AssertDecrypt(api, privKey, originals[i]); err != nil {
			FrontendError(api, "failed to assert decrypt", err)
		}
	}
}

// EncryptedZero returns a new ballot with all fields set to the encrypted
// zero point using the provided encryption key and k.
func (b *Ballot) EncryptedZero(api frontend.API, encKey EncryptionKey[frontend.Variable], k frontend.Variable) *Ballot {
	pubKey := tweds.Point{
		X: encKey.PubKey[0],
		Y: encKey.PubKey[1],
	}
	for i := range b {
		b[i] = elgamal.EncryptedZero(api, pubKey, k)
	}
	return b
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
	api.AssertIsEqual(z.IsEqual(api, x), 1)
}

func (z *Ballot) IsEqual(api frontend.API, x *Ballot) frontend.Variable {
	res := frontend.Variable(1)
	for i := range z {
		res = api.And(res, z[i].IsEqual(api, &x[i]))
	}
	return res
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
type EmulatedBallot[F emulated.FieldParams] [types.FieldsPerBallot]EmulatedCiphertext[F]

// EmulatedVote is a copy of the Vote struct, but using the emulated.Element
// type as generic type for the Address, VoteID fields and the EmulatedBallot
// type for the Ballot field.
type EmulatedVote[F emulated.FieldParams] struct {
	Address emulated.Element[F]
	VoteID  emulated.Element[F]
	Ballot  EmulatedBallot[F]
}

// Serialize returns a slice with the vote parameters in order
//
//	EmulatedVote.Address
//	EmulatedVote.VoteID
//	EmulatedVote.Ballot
func (z *EmulatedVote[F]) Serialize() []emulated.Element[F] {
	list := []emulated.Element[F]{}
	list = append(list, z.Address)
	list = append(list, z.VoteID)
	list = append(list, z.Ballot.Serialize()...)
	return list
}

// SerializeForBallotProof returns a slice with the vote parameters in order
//
//	EmulatedVote.Address
//	EmulatedVote.VoteID
//	EmulatedVote.Ballot (in Twisted Edwards format)
func (zt *EmulatedVote[F]) SerializeForBallotProof(api frontend.API) []emulated.Element[sw_bn254.ScalarField] {
	z, ok := any(zt).(*EmulatedVote[sw_bn254.ScalarField])
	if !ok {
		panic("EmulatedVote type assertion failed")
	}
	list := []emulated.Element[sw_bn254.ScalarField]{}
	list = append(list, z.Address)
	list = append(list, z.VoteID)
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
		c1xTE, c1yTE, err := emu_tweds.FromEmulatedRTEtoTE(api, zi.C1.X, zi.C1.Y)
		if err != nil {
			FrontendError(api, "failed to convert coords to RTE", err)
		}
		c2xTE, c2yTE, err := emu_tweds.FromEmulatedRTEtoTE(api, zi.C2.X, zi.C2.Y)
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

// NextK uses the MiMC hasher to generate a new k starting from the provided k.
//
// TODO: this should really be renamed MiMC7Hash, a generic func that
// uses native or emulated mimc7.NewMiMC depending on the args passed,
// and thus can be reused in all circuits
func NextK(api frontend.API, k frontend.Variable) frontend.Variable {
	kHasher, err := mimc7.NewMiMC(api)
	if err != nil {
		FrontendError(api, "failed to create MiMC hasher", err)
		return nil
	}
	if err := kHasher.Write(k); err != nil {
		FrontendError(api, "failed to write k to MiMC hasher", err)
		return nil
	}
	return kHasher.Sum()
}

func varToEmulatedElementBN254(api frontend.API, v frontend.Variable) *emulated.Element[sw_bn254.ScalarField] {
	elem, err := utils.UnpackVarToScalar[sw_bn254.ScalarField](api, v)
	if err != nil {
		panic(err)
	}
	return elem
}
