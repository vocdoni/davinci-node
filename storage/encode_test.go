package storage

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

type testEncodeData struct {
	DataStr   string
	DataInt   int
	DataFloat float64
	DataBool  bool
	DataMap   map[string]string
}

func (a *testEncodeData) Equal(b testEncodeData) bool {
	equalMap := false
	for k, v := range a.DataMap {
		if b.DataMap[k] != v {
			equalMap = false
			break
		}
		equalMap = true
	}

	return a.DataStr == b.DataStr &&
		a.DataInt == b.DataInt &&
		a.DataFloat == b.DataFloat &&
		a.DataBool == b.DataBool &&
		equalMap
}

func TestEncodeDecodeArtifact(t *testing.T) {
	c := qt.New(t)
	artifact := testEncodeData{
		DataStr:   "test",
		DataInt:   42,
		DataFloat: 3.14,
		DataBool:  true,
		DataMap:   map[string]string{"key": "value"},
	}

	c.Run("default encoding", func(c *qt.C) {
		encoded, err := EncodeArtifact(artifact)
		c.Assert(err, qt.IsNil)
		var decoded testEncodeData
		c.Assert(DecodeArtifact(encoded, &decoded), qt.IsNil)
		c.Assert(decoded.Equal(artifact), qt.IsTrue)
	})

	c.Run("cbor encoding", func(c *qt.C) {
		encoded, err := EncodeArtifact(artifact, ArtifactEncodingCBOR)
		c.Assert(err, qt.IsNil)
		var decoded testEncodeData
		c.Assert(DecodeArtifact(encoded, &decoded, ArtifactEncodingCBOR), qt.IsNil)
		c.Assert(decoded.Equal(artifact), qt.IsTrue)
	})

	c.Run("json encoding", func(c *qt.C) {
		encoded, err := EncodeArtifact(artifact, ArtifactEncodingJSON)
		c.Assert(err, qt.IsNil)
		var decoded testEncodeData
		c.Assert(DecodeArtifact(encoded, &decoded, ArtifactEncodingJSON), qt.IsNil)
		c.Assert(decoded.Equal(artifact), qt.IsTrue)
	})

	c.Run("invalid encoding", func(c *qt.C) {
		encoded, err := EncodeArtifact(artifact, ArtifactEncoding(100))
		c.Assert(err, qt.IsNotNil)
		var decoded testEncodeData
		c.Assert(DecodeArtifact(encoded, &decoded, ArtifactEncoding(100)), qt.IsNotNil)
	})
}
