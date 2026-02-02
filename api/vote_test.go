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
