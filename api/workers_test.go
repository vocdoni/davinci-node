package api

import (
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/google/uuid"
)

func TestAPI_matchesSequencerUUID(t *testing.T) {
	c := qt.New(t)

	sequencerUUID := uuid.New()
	api := &API{
		sequencerUUID: &sequencerUUID,
	}

	c.Assert(api.matchesSequencerUUID(sequencerUUID.String()), qt.IsTrue)
	c.Assert(api.matchesSequencerUUID(uuid.New().String()), qt.IsFalse)
	c.Assert(api.matchesSequencerUUID("not-a-uuid"), qt.IsFalse)
	c.Assert(api.matchesSequencerUUID(strings.Repeat("a", 4096)), qt.IsFalse)
}
