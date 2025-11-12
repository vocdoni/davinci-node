package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

// setURLParam is a helper function to set chi URL parameters in tests
func setURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCensusAPI(t *testing.T) {
	c := qt.New(t)

	// Create a temporary database
	tempDir := t.TempDir()
	kv, err := metadb.New(db.TypePebble, tempDir)
	c.Assert(err, qt.IsNil)
	defer kv.Close()

	// Create storage
	stg := storage.New(kv)

	// Create API instance
	api := &API{
		storage: stg,
		network: "sepolia",
	}

	t.Run("CreateCensus", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, NewCensusEndpoint, nil)
		c.Assert(err, qt.IsNil)

		rr := httptest.NewRecorder()
		api.newCensus(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		var response NewCensus
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		c.Assert(err, qt.IsNil)
		c.Assert(response.Census, qt.Not(qt.Equals), uuid.Nil)

		// Verify census exists in storage
		exists := stg.CensusDB().Exists(response.Census)
		c.Assert(exists, qt.IsTrue)
	})

	t.Run("AddParticipants", func(t *testing.T) {
		// Create a new census
		censusID := uuid.New()
		_, err := stg.CensusDB().New(censusID)
		c.Assert(err, qt.IsNil)

		// Prepare participants
		participants := CensusParticipants{
			Participants: []*CensusParticipant{
				{
					Key:    util.RandomBytes(20),
					Weight: new(types.BigInt).SetUint64(100),
				},
				{
					Key:    util.RandomBytes(20),
					Weight: new(types.BigInt).SetUint64(200),
				},
				{
					Key:    util.RandomBytes(20),
					Weight: new(types.BigInt).SetUint64(300),
				},
			},
		}

		body, err := json.Marshal(participants)
		c.Assert(err, qt.IsNil)

		endpoint := EndpointWithParam(AddCensusParticipantsEndpoint, CensusURLParam, censusID.String())
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		c.Assert(err, qt.IsNil)

		// Mock chi URL params
		req = setURLParam(req, CensusURLParam, censusID.String())

		rr := httptest.NewRecorder()
		api.addCensusParticipants(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		// Verify census size
		ref, err := stg.CensusDB().Load(censusID)
		c.Assert(err, qt.IsNil)
		c.Assert(ref.Size(), qt.Equals, 3)
	})

	t.Run("AddParticipantsWithDefaultWeight", func(t *testing.T) {
		// Create a new census
		censusID := uuid.New()
		_, err := stg.CensusDB().New(censusID)
		c.Assert(err, qt.IsNil)

		// Prepare participants without weight (should default to 1)
		participants := CensusParticipants{
			Participants: []*CensusParticipant{
				{
					Key: util.RandomBytes(20),
					// Weight is nil, should default to 1
				},
			},
		}

		body, err := json.Marshal(participants)
		c.Assert(err, qt.IsNil)

		endpoint := EndpointWithParam(AddCensusParticipantsEndpoint, CensusURLParam, censusID.String())
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		c.Assert(err, qt.IsNil)
		req = setURLParam(req, CensusURLParam, censusID.String())

		rr := httptest.NewRecorder()
		api.addCensusParticipants(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)
	})

	t.Run("GetCensusRoot", func(t *testing.T) {
		// Create a new census with participants
		censusID := uuid.New()
		ref, err := stg.CensusDB().New(censusID)
		c.Assert(err, qt.IsNil)

		// Add a participant
		key := util.RandomBytes(20)
		weight := new(types.BigInt).SetUint64(100)
		err = ref.Insert(key, weight.Bytes())
		c.Assert(err, qt.IsNil)

		endpoint := EndpointWithParam(GetCensusRootEndpoint, CensusURLParam, censusID.String())
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		c.Assert(err, qt.IsNil)
		req = setURLParam(req, CensusURLParam, censusID.String())

		rr := httptest.NewRecorder()
		api.getCensusRoot(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		var response types.CensusRoot
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		c.Assert(err, qt.IsNil)
		c.Assert(len(response.Root), qt.Not(qt.Equals), 0)
	})

	t.Run("GetCensusSizeByID", func(t *testing.T) {
		// Create a new census with participants
		censusID := uuid.New()
		ref, err := stg.CensusDB().New(censusID)
		c.Assert(err, qt.IsNil)

		// Add multiple participants
		for i := 0; i < 5; i++ {
			key := util.RandomBytes(20)
			weight := new(types.BigInt).SetUint64(uint64(i + 1))
			err = ref.Insert(key, weight.Bytes())
			c.Assert(err, qt.IsNil)
		}

		endpoint := EndpointWithParam(GetCensusSizeEndpoint, CensusURLParam, censusID.String())
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		c.Assert(err, qt.IsNil)
		req = setURLParam(req, CensusURLParam, censusID.String())

		rr := httptest.NewRecorder()
		api.getCensusSize(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		c.Assert(err, qt.IsNil)
		c.Assert(int(response["size"].(float64)), qt.Equals, 5)
	})

	t.Run("GetCensusSizeByRoot", func(t *testing.T) {
		// Create a new census with participants
		censusID := uuid.New()
		ref, err := stg.CensusDB().New(censusID)
		c.Assert(err, qt.IsNil)

		// Add participants
		for i := 0; i < 3; i++ {
			key := util.RandomBytes(20)
			weight := new(types.BigInt).SetUint64(uint64(i + 1))
			err = ref.Insert(key, weight.Bytes())
			c.Assert(err, qt.IsNil)
		}

		root := ref.Root()
		c.Assert(len(root), qt.Not(qt.Equals), 0)

		// Publish census by root
		destRef, err := stg.CensusDB().NewByRoot(root)
		c.Assert(err, qt.IsNil)
		err = stg.CensusDB().PublishCensus(censusID, destRef)
		c.Assert(err, qt.IsNil)

		// Get size by root
		rootHex := hex.EncodeToString(root)
		endpoint := EndpointWithParam(GetCensusSizeEndpoint, CensusURLParam, rootHex)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		c.Assert(err, qt.IsNil)
		req = setURLParam(req, CensusURLParam, rootHex)

		rr := httptest.NewRecorder()
		api.getCensusSize(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		c.Assert(err, qt.IsNil)
		c.Assert(int(response["size"].(float64)), qt.Equals, 3)
	})

	t.Run("GetCensusProof", func(t *testing.T) {
		// Create a new census with participants
		censusID := uuid.New()
		ref, err := stg.CensusDB().New(censusID)
		c.Assert(err, qt.IsNil)

		// Add a participant
		key := util.RandomBytes(20)
		weight := new(types.BigInt).SetUint64(100)
		err = ref.Insert(key, weight.Bytes())
		c.Assert(err, qt.IsNil)

		root := ref.Root()
		c.Assert(len(root), qt.Not(qt.Equals), 0)

		// Publish census by root
		destRef, err := stg.CensusDB().NewByRoot(root)
		c.Assert(err, qt.IsNil)
		err = stg.CensusDB().PublishCensus(censusID, destRef)
		c.Assert(err, qt.IsNil)

		// Get proof
		rootHex := hex.EncodeToString(root)
		keyHex := hex.EncodeToString(key)
		endpoint := fmt.Sprintf("%s?key=%s", EndpointWithParam(GetCensusProofEndpoint, CensusURLParam, rootHex), keyHex)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		c.Assert(err, qt.IsNil)
		req = setURLParam(req, CensusURLParam, rootHex)

		rr := httptest.NewRecorder()
		api.getCensusProof(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		var proof types.CensusProof
		err = json.Unmarshal(rr.Body.Bytes(), &proof)
		c.Assert(err, qt.IsNil)

		// Verify proof structure
		c.Assert(len(proof.Root), qt.Not(qt.Equals), 0)
		c.Assert(len(proof.Value), qt.Not(qt.Equals), 0)
		// Note: Siblings can be nil/empty for a single-element tree
		c.Assert(proof.Weight, qt.Not(qt.IsNil))
		c.Assert(proof.Weight.MathBigInt().Uint64(), qt.Equals, uint64(100))
		c.Assert(proof.CensusOrigin, qt.Equals, types.CensusOriginMerkleTreeOffchainStaticV1)

		// Verify proof is valid
		isValid := stg.CensusDB().VerifyProof(&proof)
		c.Assert(isValid, qt.IsTrue)
	})

	t.Run("DeleteCensus", func(t *testing.T) {
		// Create a new census
		censusID := uuid.New()
		_, err := stg.CensusDB().New(censusID)
		c.Assert(err, qt.IsNil)

		// Verify it exists
		exists := stg.CensusDB().Exists(censusID)
		c.Assert(exists, qt.IsTrue)

		// Delete it
		endpoint := EndpointWithParam(DeleteCensusEndpoint, CensusURLParam, censusID.String())
		req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
		c.Assert(err, qt.IsNil)
		req = setURLParam(req, CensusURLParam, censusID.String())

		rr := httptest.NewRecorder()
		api.deleteCensus(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		// Verify it no longer exists
		exists = stg.CensusDB().Exists(censusID)
		c.Assert(exists, qt.IsFalse)
	})

	t.Run("ErrorCases", func(t *testing.T) {
		t.Run("InvalidCensusID", func(t *testing.T) {
			endpoint := EndpointWithParam(GetCensusRootEndpoint, CensusURLParam, "invalid-uuid")
			req, err := http.NewRequest(http.MethodGet, endpoint, nil)
			c.Assert(err, qt.IsNil)
			req = setURLParam(req, CensusURLParam, "invalid-uuid")

			rr := httptest.NewRecorder()
			api.getCensusRoot(rr, req)

			c.Assert(rr.Code, qt.Equals, http.StatusBadRequest)
		})

		t.Run("CensusNotFound", func(t *testing.T) {
			nonExistentID := uuid.New()
			endpoint := EndpointWithParam(GetCensusRootEndpoint, CensusURLParam, nonExistentID.String())
			req, err := http.NewRequest(http.MethodGet, endpoint, nil)
			c.Assert(err, qt.IsNil)
			req = setURLParam(req, CensusURLParam, nonExistentID.String())

			rr := httptest.NewRecorder()
			api.getCensusRoot(rr, req)

			c.Assert(rr.Code, qt.Equals, http.StatusInternalServerError)
		})

		t.Run("EmptyParticipantsList", func(t *testing.T) {
			censusID := uuid.New()
			_, err := stg.CensusDB().New(censusID)
			c.Assert(err, qt.IsNil)

			participants := CensusParticipants{
				Participants: []*CensusParticipant{},
			}

			body, err := json.Marshal(participants)
			c.Assert(err, qt.IsNil)

			endpoint := EndpointWithParam(AddCensusParticipantsEndpoint, CensusURLParam, censusID.String())
			req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
			c.Assert(err, qt.IsNil)
			req = setURLParam(req, CensusURLParam, censusID.String())

			rr := httptest.NewRecorder()
			api.addCensusParticipants(rr, req)

			c.Assert(rr.Code, qt.Equals, http.StatusBadRequest)
		})

		t.Run("KeyLengthExceeded", func(t *testing.T) {
			censusID := uuid.New()
			_, err := stg.CensusDB().New(censusID)
			c.Assert(err, qt.IsNil)

			// Create a key that's too long (> 20 bytes)
			participants := CensusParticipants{
				Participants: []*CensusParticipant{
					{
						Key:    util.RandomBytes(types.CensusKeyMaxLen + 1),
						Weight: new(types.BigInt).SetUint64(100),
					},
				},
			}

			body, err := json.Marshal(participants)
			c.Assert(err, qt.IsNil)

			endpoint := EndpointWithParam(AddCensusParticipantsEndpoint, CensusURLParam, censusID.String())
			req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
			c.Assert(err, qt.IsNil)
			req = setURLParam(req, CensusURLParam, censusID.String())

			rr := httptest.NewRecorder()
			api.addCensusParticipants(rr, req)

			c.Assert(rr.Code, qt.Equals, http.StatusBadRequest)
		})

		t.Run("ProofForNonExistentKey", func(t *testing.T) {
			// Create a census with one participant
			censusID := uuid.New()
			ref, err := stg.CensusDB().New(censusID)
			c.Assert(err, qt.IsNil)

			key := util.RandomBytes(20)
			weight := new(types.BigInt).SetUint64(100)
			err = ref.Insert(key, weight.Bytes())
			c.Assert(err, qt.IsNil)

			root := ref.Root()

			// Publish census
			destRef, err := stg.CensusDB().NewByRoot(root)
			c.Assert(err, qt.IsNil)
			err = stg.CensusDB().PublishCensus(censusID, destRef)
			c.Assert(err, qt.IsNil)

			// Try to get proof for a different key
			nonExistentKey := util.RandomBytes(20)
			rootHex := hex.EncodeToString(root)
			keyHex := hex.EncodeToString(nonExistentKey)
			endpoint := fmt.Sprintf("%s?key=%s", EndpointWithParam(GetCensusProofEndpoint, CensusURLParam, rootHex), keyHex)
			req, err := http.NewRequest(http.MethodGet, endpoint, nil)
			c.Assert(err, qt.IsNil)
			req = setURLParam(req, CensusURLParam, rootHex)

			rr := httptest.NewRecorder()
			api.getCensusProof(rr, req)

			c.Assert(rr.Code, qt.Equals, http.StatusNotFound)
		})
	})
}
