package types

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ProcessID is the type to identify a voting process. It is composed of:
// - Address (20 bytes)
// - Version keccak(chainID + contractAddress) (4 bytes)
// - Nonce (8 bytes)
// TODO: change to []byte wrapper, it will simplify the code and SetBytes() can be removed
type ProcessID struct {
	Address common.Address // 20 bytes
	Version []byte         // first 4 bytes of keccak(chainID + contractAddress)
	Nonce   uint64         // 8 bytes big-endian
}

// SetBytes decodes bytes to ProcessId and returns the pointer to the ProcessId.
// This method is useful for chaining calls. However it does not return an error, so it should be used with caution.
// If the data is invalid, it will return a new ProcessId with zero values.
func (p *ProcessID) SetBytes(data []byte) *ProcessID {
	if err := p.Unmarshal(data); err != nil {
		return &ProcessID{
			Version: make([]byte, 4),
			Address: common.Address{},
			Nonce:   0,
		}
	}
	return p
}

// IsValid checks if the ProcessID is valid.
// A valid ProcessID must have a non-zero Address, Version, and Nonce
func (p *ProcessID) IsValid() bool {
	return p != nil && !bytes.Equal(p.Version, make([]byte, 4)) && !bytes.Equal(p.Address.Bytes(), common.Address{}.Bytes())
}

// BigInt returns a BigInt representation of the ProcessId.
func (p *ProcessID) BigInt() *big.Int {
	if p == nil {
		return nil
	}
	return new(big.Int).SetBytes(p.Marshal())
}

// Marshal encodes ProcessId to bytes:
//
//	Address (20 bytes) | Version (4 bytes) | Nonce (8 bytes)
func (p *ProcessID) Marshal() []byte {
	version := make([]byte, 4)
	copy(version, p.Version)
	nonce := make([]byte, 8)
	binary.BigEndian.PutUint64(nonce, p.Nonce)

	var id bytes.Buffer
	id.Write(p.Address.Bytes()[:20])
	id.Write(version)
	id.Write(nonce[:8])
	return id.Bytes()
}

// UnMarshal decodes bytes to ProcessId.
func (p *ProcessID) Unmarshal(data []byte) error {
	if len(data) != 32 {
		return fmt.Errorf("invalid ProcessID length: %d", len(data))
	}
	p.Address = common.BytesToAddress(data[:20])
	p.Version = data[20:24]
	p.Nonce = binary.BigEndian.Uint64(data[24:32])
	return nil
}

// MarshalBinary implements the BinaryMarshaler interface
func (p *ProcessID) MarshalBinary() (data []byte, err error) {
	return p.Marshal(), nil
}

// UnmarshalBinary implements the BinaryMarshaler interface
func (p *ProcessID) UnmarshalBinary(data []byte) error {
	return p.Unmarshal(data)
}

// String returns a human readable representation of process ID
func (p *ProcessID) String() string {
	return hex.EncodeToString(p.Marshal())
}

// HasVersion checks if the ProcessID version matches the given version
func (p *ProcessID) HasVersion(version []byte) bool {
	return bytes.Equal(p.Version, version)
}

// TestProcessID is a deterministic ProcessID used for testing purposes.
// All circuit tests should use this ProcessID to ensure consistent caching
// and proof reuse between tests.
var TestProcessID = &ProcessID{
	Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
	Version: []byte{0x00, 0x00, 0x00, 0x01},
	Nonce:   1,
}

// ProcessIDVersion computes the version for a ProcessID. It is defined as the
// first 4 bytes of the Keccak-256 hash of the concatenation of the chain ID
// (4 bytes big-endian) and the contract address (20 bytes).
func ProcessIDVersion(chainID uint32, contractAddr common.Address) []byte {
	var buf [24]byte
	// chainId: 4 bytes big-endian
	binary.BigEndian.PutUint32(buf[0:4], chainID)
	// address: 20 raw bytes
	copy(buf[4:], contractAddr.Bytes())
	// Keccak-256 (Ethereum's legacy Keccak, not NIST SHA3)
	sum := crypto.Keccak256(buf[:])
	// Take the last 4 bytes (least-significant 32 bits) as the version
	return sum[len(sum)-4:]
}
