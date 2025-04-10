package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func (a *API) newCensus(w http.ResponseWriter, r *http.Request) {
	censusID := uuid.New()
	_, err := a.storage.CensusDB().New(censusID)
	if err != nil {
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}
	httpWriteJSON(w, &NewCensus{Census: censusID})
}

func (a *API) addCensusParticipants(w http.ResponseWriter, r *http.Request) {
	censusID, err := uuid.Parse(chi.URLParam(r, CensusURLParam))
	if err != nil {
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}

	var participants CensusParticipants
	if err := json.NewDecoder(r.Body).Decode(&participants); err != nil {
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}

	if len(participants.Participants) == 0 {
		ErrMalformedBody.WithErr(fmt.Errorf("no participants provided")).Write(w)
		return
	}

	ref, err := a.storage.CensusDB().Load(censusID)
	if err != nil {
		ErrCensusNotFound.WithErr(err).Write(w)
		return
	}

	// build the list of keys and values that will be added to the tree
	keys := [][]byte{}
	values := [][]byte{}
	for _, p := range participants.Participants {
		if p.Weight == nil {
			p.Weight = new(types.BigInt).SetUint64(1)
		}
		leafKey := p.Key
		if len(p.Key) > types.CensusKeyMaxLen {
			leafKey = a.storage.CensusDB().HashAndTrunkKey(p.Key)
			if leafKey == nil {
				ErrGenericInternalServerError.WithErr(fmt.Errorf("failed to hash participant key")).Write(w)
				return
			}
		}
		keys = append(keys, leafKey)
		values = append(values, arbo.BigIntToBytes(a.storage.CensusDB().HashLen(), p.Weight.MathBigInt()))
	}

	// insert the keys and values into the tree
	invalid, err := ref.InsertBatch(keys, values)
	if err != nil {
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}
	if len(invalid) > 0 {
		ErrMalformedBody.WithErr(fmt.Errorf("failed to insert %d participants", len(invalid))).Write(w)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) getCensusParticipants(w http.ResponseWriter, r *http.Request) {
	censusID, err := uuid.Parse(chi.URLParam(r, CensusURLParam))
	if err != nil {
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}

	ref, err := a.storage.CensusDB().Load(censusID)
	if err != nil {
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	// TODO: Implement pagination properly
	keys := make([][]byte, 0)
	values := make([][]byte, 0)
	err = ref.Tree().Iterate(nil, func(k []byte, v []byte) {
		keys = append(keys, k)
		values = append(values, v)
	})
	if err != nil {
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	participants := make([]CensusParticipant, len(keys))
	for i := range keys {
		participants[i] = CensusParticipant{
			Key:    keys[i],
			Weight: (*types.BigInt)(arbo.BytesToBigInt(values[i])),
		}
	}

	httpWriteJSON(w, map[string]interface{}{
		"participants": participants,
	})
}

func (a *API) getCensusRoot(w http.ResponseWriter, r *http.Request) {
	censusID, err := uuid.Parse(chi.URLParam(r, CensusURLParam))
	if err != nil {
		ErrInvalidCensusID.WithErr(err).Write(w)
		return
	}

	ref, err := a.storage.CensusDB().Load(censusID)
	if err != nil {
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	httpWriteJSON(w, map[string]types.HexBytes{
		"root": types.HexBytes(ref.Root()),
	})
}

func (a *API) getCensusSize(w http.ResponseWriter, r *http.Request) {
	size := 0
	if censusID, err := uuid.Parse(chi.URLParam(r, CensusURLParam)); err == nil {
		ref, err := a.storage.CensusDB().Load(censusID)
		if err != nil {
			ErrGenericInternalServerError.WithErr(err).Write(w)
			return
		}
		size = ref.Size()
	} else if root, err := hex.DecodeString(chi.URLParam(r, CensusURLParam)); err == nil {
		if size, err = a.storage.CensusDB().SizeByRoot(root); err != nil {
			ErrGenericInternalServerError.WithErr(err).Write(w)
			return
		}
	} else {
		ErrInvalidCensusID.WithErr(err).Write(w)
		return
	}
	httpWriteJSON(w, map[string]interface{}{
		"size": size,
	})
}

func (a *API) deleteCensus(w http.ResponseWriter, r *http.Request) {
	censusID, err := uuid.Parse(chi.URLParam(r, CensusURLParam))
	if err != nil {
		ErrInvalidCensusID.WithErr(err).Write(w)
		return
	}

	if err := a.storage.CensusDB().Del(censusID); err != nil {
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a *API) getCensusProof(w http.ResponseWriter, r *http.Request) {
	rootHex := chi.URLParam(r, CensusURLParam)
	root, err := hex.DecodeString(rootHex)
	if err != nil {
		ErrInvalidCensusID.WithErr(err).Write(w)
		return
	}

	keyHex := r.URL.Query().Get("key")
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}

	leafKey := key
	if len(key) > types.CensusKeyMaxLen {
		leafKey = a.storage.CensusDB().HashAndTrunkKey(key)
		if leafKey == nil {
			ErrGenericInternalServerError.WithErr(fmt.Errorf("failed to hash participant key")).Write(w)
			return
		}
	}

	proof, err := a.storage.CensusDB().ProofByRoot(root, leafKey)
	if err != nil {
		ErrResourceNotFound.WithErr(err).Write(w)
		return
	}

	httpWriteJSON(w, proof)
}
