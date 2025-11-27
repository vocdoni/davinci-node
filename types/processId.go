package types

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/util"
)

// ProcessID is the type to identify a voting process. It is composed of:
//   - Address (20 bytes)
//   - Version keccak(chainID + contractAddress) (4 bytes)
//   - Nonce (8 bytes, big-endian)
type ProcessID [ProcessIDLen]byte

// ProcessIDLen is the length in bytes of a ProcessID
const ProcessIDLen = 32

// NewProcessID builds a ProcessID using the passed params.
func NewProcessID(addr common.Address, version [4]byte, nonce uint64) ProcessID {
	var pid ProcessID
	copy(pid[0:20], addr.Bytes())
	copy(pid[20:24], version[:])
	binary.BigEndian.PutUint64(pid[24:32], nonce)
	return pid
}

// ParseProcessIDHex parses a ProcessID from a hex string.
// It accepts optional "0x" prefix and requires exactly 32 bytes (64 hex chars).
func ParseProcessIDHex(s string) (ProcessID, error) {
	s = util.TrimHex(s) // strips 0x if present

	if len(s) != ProcessIDLen*2 {
		return ProcessID{}, fmt.Errorf("invalid process ID hex length %d, want %d", len(s), ProcessIDLen*2)
	}

	b, err := hex.DecodeString(s)
	if err != nil {
		return ProcessID{}, fmt.Errorf("could not decode hex string: %w", err)
	}

	var pid ProcessID
	if err := pid.UnmarshalBinary(b); err != nil {
		return ProcessID{}, err
	}
	return pid, nil
}

func ProcessIDFromBytes(data []byte) (ProcessID, error) {
	var processID ProcessID
	if err := processID.UnmarshalBinary(data); err != nil {
		return ProcessID{}, err
	}
	return processID, nil
}

func ProcessIDFromBigInt(bi *big.Int) (ProcessID, error) {
	if bi == nil {
		return ProcessID{}, fmt.Errorf("nil big.Int")
	}
	var processID ProcessID
	bi.FillBytes(processID[:])
	return processID, nil
}

func (p ProcessID) Address() common.Address { return common.BytesToAddress(p[0:20]) }
func (p ProcessID) Version() [4]byte        { var v [4]byte; copy(v[:], p[20:24]); return v }
func (p ProcessID) Nonce() uint64           { return binary.BigEndian.Uint64(p[24:32]) }

// IsValid checks if the ProcessID is valid.
// A valid ProcessID must have a non-zero Address and Version
func (p ProcessID) IsValid() bool {
	if p.Address().Cmp(common.Address{}) == 0 ||
		p.Version() == [4]byte{} {
		return false
	}
	return true
}

// MathBigInt returns a *math/big.Int representation of the ProcessId.
func (p ProcessID) MathBigInt() *big.Int { return new(big.Int).SetBytes(p[:]) }

// BigInt returns a types.BigInt representation of the ProcessId.
func (p ProcessID) BigInt() *BigInt { return NewInt(0).SetBytes(p[:]) }

// Bytes returns a slice view of the underlying array.
func (p ProcessID) Bytes() []byte { return p[:] }

// String returns a human readable representation of process ID
func (p ProcessID) String() string { return hex.EncodeToString(p[:]) }

// MarshalBinary implements the BinaryMarshaler interface
func (p ProcessID) MarshalBinary() (data []byte, err error) { return p[:], nil }

// UnmarshalBinary implements the BinaryMarshaler interface
func (p *ProcessID) UnmarshalBinary(data []byte) error {
	if len(data) != ProcessIDLen {
		return fmt.Errorf("invalid ProcessID length: %d", len(data))
	}
	copy(p[:], data)
	return nil
}

// ToFF returns the finite field representation of the ProcessID.
// It uses the curve scalar field to represent the ProcessID.
func (p *ProcessID) ToFF(baseField *big.Int) ProcessID {
	bi := crypto.BigToFF(baseField, p.MathBigInt())
	bi.FillBytes(p[:])
	return *p
}

// ProcessIDVersion computes the version for a ProcessID. It is defined as the
// last 4 bytes of the Keccak-256 hash of the concatenation of the chain ID
// (4 bytes big-endian) and the contract address (20 bytes).
func ProcessIDVersion(chainID uint32, contractAddr common.Address) [4]byte {
	var buf [24]byte
	// chainId: 4 bytes big-endian
	binary.BigEndian.PutUint32(buf[0:4], chainID)
	// address: 20 raw bytes
	copy(buf[4:], contractAddr.Bytes())
	// Keccak-256 (Ethereum's legacy Keccak, not NIST SHA3)
	sum := ethcrypto.Keccak256(buf[:])
	// Take the last 4 bytes (least-significant 32 bits) as the version
	var v [4]byte
	copy(v[:], sum[len(sum)-4:]) // last 4 bytes
	return v
}
