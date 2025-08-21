package sequencer

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestParseSequencerURL(t *testing.T) {
	c := qt.New(t)

	expectedBase := "http://example.com"
	expectedUUID := "my-uuid"

	base, uuid, err := parseSequencerURL("http://example.com/workers/my-uuid/other-data")
	c.Assert(err, qt.IsNil)
	c.Assert(base, qt.Equals, expectedBase)
	c.Assert(uuid, qt.Equals, expectedUUID)

	expectedBase = "http://example.com/api"
	base, uuid, err = parseSequencerURL("http://example.com/api/workers/my-uuid")
	c.Assert(err, qt.IsNil)
	c.Assert(base, qt.Equals, expectedBase)
	c.Assert(uuid, qt.Equals, expectedUUID)

	base, uuid, err = parseSequencerURL("http://example.com/api/workers/my-uuid?param1=value1&param2=value2")
	c.Assert(err, qt.IsNil)
	c.Assert(base, qt.Equals, expectedBase)
	c.Assert(uuid, qt.Equals, expectedUUID)
}
