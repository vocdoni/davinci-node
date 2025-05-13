package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
)

// newProcess creates a new voting process
// POST /process
func (a *API) newProcess(w http.ResponseWriter, r *http.Request) {
	p := &types.ProcessSetup{}
	if err := json.NewDecoder(r.Body).Decode(p); err != nil {
		ErrMalformedBody.Withf("could not decode request body: %v", err).Write(w)
		return
	}
	// Extract the address from the signature
	address, err := ethereum.AddrFromSignature(fmt.Appendf(nil, "%d%d", p.ChainID, p.Nonce), new(ethereum.ECDSASignature).SetBytes(p.Signature))
	if err != nil {
		ErrInvalidSignature.Withf("could not extract address from signature: %v", err).Write(w)
		return
	}

	// Validate the ballot mode
	if err := p.BallotMode.Validate(); err != nil {
		ErrMalformedBody.Withf("invalid ballot mode: %v", err).Write(w)
		return
	}

	// Create the process ID
	pid := types.ProcessID{
		Address: address,
		Nonce:   p.Nonce,
		ChainID: p.ChainID,
	}

	// Generate the elgamal key
	publicKey, privateKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	if err != nil {
		ErrGenericInternalServerError.Withf("could not generate elgamal key: %v", err).Write(w)
		return
	}
	x, y := publicKey.Point()

	// Store the encryption keys or retrieve them if they already exist
	if err := a.storage.SetEncryptionKeys(&pid, publicKey, privateKey); err != nil {
		if errors.Is(err, storage.ErrKeyAlreadyExists) {
			pub, _, err := a.storage.EncryptionKeys(&pid)
			if err != nil {
				ErrGenericInternalServerError.Withf("could not retrieve encryption keys: %v", err).Write(w)
				return
			}
			x, y = pub.Point()
		} else {
			ErrGenericInternalServerError.Withf("could not store encryption keys: %v", err).Write(w)
			return
		}
	}
	// prepare inputs for the state ready for the state transition circuit:
	// - the process ID that should be in the scalar field of the circuit curve
	// - the census root that should be in encoded according to the arbo format
	ffPID := crypto.BigToFF(circuits.StateTransitionCurve.ScalarField(), pid.BigInt())
	bigCensusRoot := arbo.BytesToBigInt(p.CensusRoot)
	// Initialize the state
	st, err := state.New(a.storage.StateDB(), ffPID)
	if err != nil {
		ErrGenericInternalServerError.Withf("could not create state: %v", err).Write(w)
		return
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Warnw("failed to close state", "error", err)
		}
	}()

	// Initialize the state with the census root and the encryption key
	// If the state is already initialized, we ignore the error and continue with the process setup
	if err := st.Initialize(bigCensusRoot,
		circuits.BallotModeToCircuit(p.BallotMode),
		circuits.EncryptionKeyFromECCPoint(publicKey)); err != nil {
		if !errors.Is(err, state.ErrStateAlreadyInitialized) {
			ErrGenericInternalServerError.Withf("could not initialize state: %v", err).Write(w)
			return
		}
	}

	root, err := st.RootAsBigInt()
	if err != nil {
		ErrGenericInternalServerError.Withf("could not get state root: %v", err).Write(w)
		return
	}

	// Create the process response
	pr := &types.ProcessSetupResponse{
		ProcessID:        pid.Marshal(),
		EncryptionPubKey: [2]types.BigInt{types.BigInt(*x), types.BigInt(*y)},
		StateRoot:        root.Bytes(),
		BallotMode:       p.BallotMode,
	}

	// Write the response
	log.Infow("new process setup query",
		"address", address.String(),
		"processId", pr.ProcessID.String(),
		"pubKeyX", pr.EncryptionPubKey[0].String(),
		"pubKeyY", pr.EncryptionPubKey[1].String(),
		"stateRoot", pr.StateRoot.String(),
		"ballotMode", pr.BallotMode.String(),
	)
	httpWriteJSON(w, pr)
}

// getProcess retrieves a voting process
// GET /process/{processId}
func (a *API) process(w http.ResponseWriter, r *http.Request) {
	// Unmarshal the process ID
	pidBytes, err := hex.DecodeString(util.TrimHex(chi.URLParam(r, ProcessURLParam)))
	if err != nil {
		ErrMalformedProcessID.Withf("could not decode process ID: %v", err).Write(w)
		return
	}
	pid := types.ProcessID{}
	if err := pid.Unmarshal(pidBytes); err != nil {
		ErrMalformedProcessID.Withf("could not unmarshal process ID: %v", err).Write(w)
		return
	}

	// Retrieve the process
	proc, err := a.storage.Process(&pid)
	if err != nil {
		ErrProcessNotFound.Withf("could not retrieve process: %v", err).Write(w)
		return
	}

	// Write the response
	httpWriteJSON(w, proc)
}
