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
// - Prefix keccak(chainID + contractAddress) (4 bytes)
// - Address (20 bytes)
// - Nonce (8 bytes)
// TODO: change to []byte wrapper, it will simplify the code and SetBytes() can be removed
type ProcessID struct {
	Prefix  []byte         // first 4 bytes of keccak(chainID + contractAddress)
	Address common.Address // 20 bytes
	Nonce   uint64         // 8 bytes big-endian
}

// SetBytes decodes bytes to ProcessId and returns the pointer to the ProcessId.
// This method is useful for chaining calls. However it does not return an error, so it should be used with caution.
// If the data is invalid, it will return a new ProcessId with zero values.
func (p *ProcessID) SetBytes(data []byte) *ProcessID {
	if err := p.Unmarshal(data); err != nil {
		return &ProcessID{
			Prefix:  make([]byte, 4),
			Address: common.Address{},
			Nonce:   0,
		}
	}
	return p
}

// IsValid checks if the ProcessID is valid.
// A valid ProcessID must have a non-zero Prefix, Address, and Nonce
func (p *ProcessID) IsValid() bool {
	return p != nil && !bytes.Equal(p.Prefix, make([]byte, 4)) && !bytes.Equal(p.Address.Bytes(), common.Address{}.Bytes())
}

// BigInt returns a BigInt representation of the ProcessId.
func (p *ProcessID) BigInt() *big.Int {
	if p == nil {
		return nil
	}
	return new(big.Int).SetBytes(p.Marshal())
}

// Marshal encodes ProcessId to bytes:
func (p *ProcessID) Marshal() []byte {
	prefix := make([]byte, 4)
	copy(prefix, p.Prefix)
	nonce := make([]byte, 8)
	binary.BigEndian.PutUint64(nonce, p.Nonce)

	var id bytes.Buffer
	id.Write(prefix)
	id.Write(p.Address.Bytes()[:20])
	id.Write(nonce[:8])
	return id.Bytes()
}

// UnMarshal decodes bytes to ProcessId.
func (p *ProcessID) Unmarshal(data []byte) error {
	if len(data) != 32 {
		return fmt.Errorf("invalid ProcessID length: %d", len(data))
	}
	p.Prefix = data[:4]
	p.Address = common.BytesToAddress(data[4:24])
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

// TestProcessID is a deterministic ProcessID used for testing purposes.
// All circuit tests should use this ProcessID to ensure consistent caching
// and proof reuse between tests.
var TestProcessID = &ProcessID{
	Prefix:  []byte{0x00, 0x00, 0x00, 0x01},
	Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
	Nonce:   1,
}

// ProcessIDPrefix computes the prefix for a ProcessID. It is defined as the
// first 4 bytes of the Keccak-256 hash of the concatenation of the chain ID
// (4 bytes big-endian) and the contract address (20 bytes).
func ProcessIDPrefix(chainID uint32, contractAddr common.Address) []byte {
	var buf [24]byte
	// chainId: 4 bytes big-endian
	binary.BigEndian.PutUint32(buf[0:4], chainID)
	// address: 20 raw bytes
	copy(buf[4:], contractAddr.Bytes())
	// Keccak-256 (Ethereum's legacy Keccak, not NIST SHA3)
	sum := crypto.Keccak256(buf[:])
	// Take the last 4 bytes (least-significant 32 bits) as the prefix
	return sum[len(sum)-4:]
}
