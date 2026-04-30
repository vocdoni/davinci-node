package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/types"
)

func TestSkipUnknownProcessIDMiddleware(t *testing.T) {
	versionSepolia := [4]byte{0x01, 0x02, 0x03, 0x04}
	versionArbitrum := [4]byte{0x05, 0x06, 0x07, 0x08}
	versionUnknown := [4]byte{0x09, 0x0a, 0x0b, 0x0c}

	router := chi.NewRouter()
	router.With(skipUnknownProcessIDMiddleware(map[[4]byte]struct{}{
		versionSepolia:  {},
		versionArbitrum: {},
	})).Get(ProcessEndpoint, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testCases := []struct {
		name      string
		processID types.ProcessID
		wantCode  int
	}{
		{
			name:      "accepted first version",
			processID: types.NewProcessID(common.HexToAddress("0x0000000000000000000000000000000000000004"), versionSepolia, 1),
			wantCode:  http.StatusOK,
		},
		{
			name:      "accepted second version",
			processID: types.NewProcessID(common.HexToAddress("0x0000000000000000000000000000000000000005"), versionArbitrum, 2),
			wantCode:  http.StatusOK,
		},
		{
			name:      "rejected unknown version",
			processID: types.NewProcessID(common.HexToAddress("0x0000000000000000000000000000000000000006"), versionUnknown, 3),
			wantCode:  http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)
			req := httptest.NewRequest(http.MethodGet, "/processes/"+tc.processID.String(), nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			c.Assert(rr.Code, qt.Equals, tc.wantCode)
		})
	}
}
