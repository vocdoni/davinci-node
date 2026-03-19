package metadata

import (
	"encoding/json"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

func TestCIDRoundTrip(t *testing.T) {
	c := qt.New(t)

	metadata := testMetadata()
	key, data, err := CID(metadata)
	c.Assert(err, qt.IsNil)

	expected, err := json.Marshal(metadata)
	c.Assert(err, qt.IsNil)
	c.Assert(data, qt.DeepEquals, expected)

	parsedCID, err := HexBytesToCID(key)
	c.Assert(err, qt.IsNil)
	c.Assert(CIDToHexBytes(parsedCID), qt.DeepEquals, key)
	c.Assert(CIDStringToHexBytes(parsedCID.String()), qt.DeepEquals, key)
}

func TestCIDStringToHexBytesInvalid(t *testing.T) {
	c := qt.New(t)

	c.Assert(CIDStringToHexBytes("not-a-cid"), qt.IsNil)
}

func TestHexBytesToCIDInvalid(t *testing.T) {
	c := qt.New(t)

	_, err := HexBytesToCID(types.HexBytes("not-a-cid"))
	c.Assert(err, qt.Not(qt.IsNil))
}

func TestCIDMarshalError(t *testing.T) {
	c := qt.New(t)

	_, _, err := CID(testUnsupportedMetadata())
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "marshal json")
}
