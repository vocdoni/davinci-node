package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestNewVoteInvalidCurveType(t *testing.T) {
	c := qt.New(t)
	api := &API{}
	body := `{"ballot":{"curveType":"babyjubjub","ciphertexts":[{},{},{},{},{},{},{},{}]}}`

	req := httptest.NewRequest(http.MethodPost, VotesEndpoint, strings.NewReader(body))
	rr := httptest.NewRecorder()

	api.newVote(rr, req)

	c.Assert(rr.Code, qt.Equals, http.StatusBadRequest)
	c.Assert(rr.Body.String(), qt.Contains, "invalid curve type:")
}

func TestNewVoteRejectsNonCanonicalAddressLength(t *testing.T) {
	c := qt.New(t)
	api := &API{}
	body := `{
		"ballot":{"curveType":"bn254","ciphertexts":[{},{},{},{},{},{},{},{}]},
		"ballotInputsHash":"1",
		"address":"0x00112233445566778899aabbccddeeff0011223344",
		"signature":"0x01"
	}`

	req := httptest.NewRequest(http.MethodPost, VotesEndpoint, strings.NewReader(body))
	rr := httptest.NewRecorder()

	api.newVote(rr, req)

	c.Assert(rr.Code, qt.Equals, http.StatusBadRequest)
	c.Assert(rr.Body.String(), qt.Contains, "address must be 20 bytes")
}
