package types

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
)

func TestNewProcessIDUses31BytesAnd7ByteNonce(t *testing.T) {
	c := qt.New(t)

	addr := common.HexToAddress("0x11223344556677889900aabbccddeeff00112233")
	version := [4]byte{0xaa, 0xbb, 0xcc, 0xdd}
	nonce := uint64(0x0102030405060708)

	processID := NewProcessID(addr, version, nonce)

	c.Assert(ProcessIDLen, qt.Equals, 31)
	c.Assert(len(processID), qt.Equals, 31)
	c.Assert(processID[0:20], qt.DeepEquals, addr.Bytes())
	c.Assert(processID[20:24], qt.DeepEquals, version[:])
	c.Assert(processID[24:31], qt.DeepEquals, []byte{0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})
	c.Assert(processID.Nonce(), qt.Equals, uint64(0x02030405060708))
}
