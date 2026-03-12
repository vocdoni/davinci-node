package web3

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

func TestBytes32FromCensusRoot(t *testing.T) {
	c := qt.New(t)

	c.Run("left pads short roots", func(c *qt.C) {
		root := types.HexBytes{0x72, 0x4a, 0x70}

		got := bytes32FromCensusRoot(root)

		c.Assert(got[:29], qt.DeepEquals, make([]byte, 29))
		c.Assert(got[29:], qt.DeepEquals, []byte{0x72, 0x4a, 0x70})
	})

	c.Run("preserves thirty two byte roots", func(c *qt.C) {
		root := make(types.HexBytes, 32)
		for i := range root {
			root[i] = byte(i + 1)
		}

		got := bytes32FromCensusRoot(root)

		c.Assert(got[:], qt.DeepEquals, []byte(root))
	})
}
