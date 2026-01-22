package types

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/spec/params"
)

// TODO: implement CensusIndex and remove this
const IndexTODO = 0

type (
	StateKey    uint64
	VoteID      uint64
	BallotIndex uint64
)

func HexStringToVoteID(s string) (VoteID, error) {
	k, err := hexStringToStateKey("VoteID", s)
	if err != nil {
		return 0, err
	}
	v := VoteID(k)
	if !v.Valid() {
		return 0, fmt.Errorf("invalid VoteID: out of range (got %s)", StateKey(v))
	}
	return v, nil
}

func BigIntToVoteID(x *big.Int) (VoteID, error) {
	k, err := bigIntToStateKey(x)
	if err != nil {
		return 0, err
	}
	v := VoteID(k)
	if !v.Valid() {
		return 0, fmt.Errorf("out of range")
	}
	return v, nil
}

// CalculateBallotIndex returns a BallotIndex on the lower half of the 64 bit space,
// BallotMin and BallotMax.
//
//	BallotIndex = BallotMin + (censusIndex * 2^CensusAddressBitLen) + (address mod 2^CensusAddressBitLen)
func CalculateBallotIndex(address *big.Int, censusIndex uint64) BallotIndex {
	ballotIndex, err := spec.BallotIndex(address, censusIndex)
	if err != nil {
		panic(err)
	}
	return BallotIndex(ballotIndex)
}

func HexStringToBallotIndex(s string) (BallotIndex, error) {
	k, err := hexStringToStateKey("BallotIndex", s)
	if err != nil {
		return 0, err
	}
	b := BallotIndex(k)
	if !b.Valid() {
		return 0, fmt.Errorf("invalid BallotIndex: out of range (got %s)", StateKey(b))
	}
	return b, nil
}

func BigIntToBallotIndex(x *big.Int) (BallotIndex, error) {
	k, err := bigIntToStateKey(x)
	if err != nil {
		return 0, err
	}
	b := BallotIndex(k)
	if !b.Valid() {
		return 0, fmt.Errorf("out of range")
	}
	return b, nil
}

func (k StateKey) Uint64() uint64                { return uint64(k) }
func (k StateKey) ToGnark() uint64               { return uint64(k) }
func (k StateKey) BigInt() *big.Int              { return new(big.Int).SetUint64(uint64(k)) }
func (k StateKey) IsInField(field *big.Int) bool { return k.BigInt().Cmp(field) < 0 }
func (k StateKey) String() string                { return "0x" + hex.EncodeToString(k.Bytes()) }
func (k StateKey) Bytes() []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(k))
	return b[:]
}

func (k StateKey) MarshalJSON() ([]byte, error) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(k))
	return HexBytes(b[:]).MarshalJSON() // fixed 8 bytes => canonical output
}

func (k *StateKey) UnmarshalJSON(data []byte) error {
	sk, err := stateKeyUnmarshalJSON("StateKey", data)
	if err != nil {
		return err
	}
	*k = StateKey(sk)
	return nil
}

func (v VoteID) StateKey() StateKey { return StateKey(v) }
func (v VoteID) Valid() bool {
	return uint64(v) >= params.VoteIDMin && uint64(v) <= params.VoteIDMax
}
func (v VoteID) Uint64() uint64                { return StateKey(v).Uint64() }
func (v VoteID) ToGnark() uint64               { return StateKey(v).ToGnark() }
func (v VoteID) BigInt() *big.Int              { return StateKey(v).BigInt() }
func (v VoteID) IsInField(field *big.Int) bool { return StateKey(v).IsInField(field) }
func (v VoteID) String() string                { return StateKey(v).String() }
func (v VoteID) Bytes() []byte                 { return StateKey(v).Bytes() }
func (v VoteID) MarshalJSON() ([]byte, error)  { return StateKey(v).MarshalJSON() }
func (v *VoteID) UnmarshalJSON(data []byte) error {
	sk, err := stateKeyUnmarshalJSON("VoteID", data)
	if err != nil {
		return err
	}
	vv := VoteID(sk)
	if !vv.Valid() {
		return fmt.Errorf("invalid VoteID: out of range (got %s)", StateKey(vv))
	}
	*v = vv
	return nil
}

func (b BallotIndex) StateKey() StateKey { return StateKey(b) }
func (b BallotIndex) Valid() bool {
	return uint64(b) >= params.BallotMin && uint64(b) <= params.BallotMax
}
func (b BallotIndex) Uint64() uint64                { return StateKey(b).Uint64() }
func (b BallotIndex) ToGnark() uint64               { return StateKey(b).ToGnark() }
func (b BallotIndex) BigInt() *big.Int              { return StateKey(b).BigInt() }
func (b BallotIndex) IsInField(field *big.Int) bool { return StateKey(b).IsInField(field) }
func (b BallotIndex) String() string                { return StateKey(b).String() }
func (b BallotIndex) Bytes() []byte                 { return StateKey(b).Bytes() }
func (b BallotIndex) MarshalJSON() ([]byte, error)  { return StateKey(b).MarshalJSON() }
func (b *BallotIndex) UnmarshalJSON(data []byte) error {
	sk, err := stateKeyUnmarshalJSON("BallotIndex", data)
	if err != nil {
		return err
	}
	bb := BallotIndex(sk)
	if !bb.Valid() {
		return fmt.Errorf("invalid BallotIndex: out of range (got %s)", StateKey(bb))
	}
	*b = bb
	return nil
}

func stateKeyUnmarshalJSON(name string, data []byte) (StateKey, error) {
	var hb HexBytes
	if err := hb.UnmarshalJSON(data); err != nil {
		return 0, err
	}
	if len(hb) == 0 {
		return 0, fmt.Errorf("invalid %s: empty", name)
	}
	if len(hb) > 8 {
		return 0, fmt.Errorf("invalid %s: too many bytes (%d)", name, len(hb))
	}

	var b [8]byte
	copy(b[8-len(hb):], hb) // left-pad to 8
	return StateKey(binary.BigEndian.Uint64(b[:])), nil
}

func hexStringToStateKey(name, s string) (StateKey, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	if s == "" {
		return 0, fmt.Errorf("invalid %s: empty", name)
	}
	u, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s hex %q: %w", name, s, err)
	}
	return StateKey(u), nil
}

func bigIntToStateKey(x *big.Int) (StateKey, error) {
	if x == nil {
		return 0, fmt.Errorf("nil big.Int")
	}
	if x.Sign() < 0 {
		return 0, fmt.Errorf("negative value")
	}
	if !x.IsUint64() {
		return 0, fmt.Errorf("overflows uint64")
	}
	return StateKey(x.Uint64()), nil
}
