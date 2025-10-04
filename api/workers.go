package api

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/workers"
)

// startWorkersAPI method checks if the workers API should be started. If so,
// it generates the sequencer signer and uuid using the seed defined in the
// provided configuration. It also starts the workers monitor and register
// the required handlers in the API router. If something fails return the
// error. If no seed is defined in the provided configuration, the workers
// API will not be started.
func (a *API) startWorkersAPI(conf APIConfig) error {
	// Initialize sequencer signer if sequencerWorkerSeed is provided
	if conf.SequencerWorkersSeed != "" {
		var err error
		// prepare the seed to be the ethereum signer private key
		// initialize the ethereum signer
		a.sequencerSigner, err = ethereum.NewSignerFromSeed([]byte(conf.SequencerWorkersSeed))
		if err != nil {
			return fmt.Errorf("failed to create worker signer: %w", err)
		}
		// calculate the sequencer UUID using the seed
		a.sequencerUUID, err = workers.WorkerSeedToUUID(conf.SequencerWorkersSeed)
		if err != nil {
			return fmt.Errorf("failed to create worker UUID: %w", err)
		}
		// Start workers monitor
		a.startWorkersMonitor()

		// Add worker endpoints
		log.Infow("register handler", "endpoint", WorkerTokenDataEndpoint, "method", "GET")
		a.router.Get(WorkerTokenDataEndpoint, a.workersTokenData)
		log.Infow("register handler", "endpoint", WorkerJobEndpoint, "method", "GET")
		a.router.Get(WorkerJobEndpoint, a.workersNewJob)
		log.Infow("register handler", "endpoint", WorkerJobEndpoint, "method", "POST")
		a.router.Post(WorkerJobEndpoint, a.workersSubmitJob)

		log.Infow("worker API enabled",
			"sequencerUUID", a.sequencerUUID.String(),
			"sequencerAddr", a.sequencerSigner.Address().Hex(),
			"workersEndpoint", EndpointWithParam(WorkersEndpoint, SequencerUUIDParam, a.sequencerUUID.String()))
	}
	return nil
}

// startWorkersMonitor starts the timeout monitor for worker jobs
func (a *API) startWorkersMonitor() {
	// Start the jobs manager with the worker job timeout and the ban rules
	a.jobsManager = workers.NewJobsManager(a.storage, a.workersJobTimeout, a.workersBanRules)
	a.jobsManager.Start(a.parentCtx)

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case failedJob := <-a.jobsManager.FailedJobs:
				now := time.Now()
				voteIDStr := hex.EncodeToString(failedJob.VoteID)
				log.Warnw("job failed or timed out",
					"voteID", voteIDStr,
					"workerAddr", failedJob.Address,
					"duration", now.Sub(failedJob.Timestamp).String())

				// Remove ballot reservation (this will put it back in the queue)
				if err := a.storage.ReleaseBallotReservation(failedJob.VoteID); err != nil {
					log.Warnw("failed to remove timed out ballot",
						"error", err.Error(),
						"voteID", voteIDStr)
				}
			case <-a.parentCtx.Done():
				log.Infow("worker timeout monitor stopped")
				return
			}
		}
	}()
}

// authWorkerFromRequest method checks that the request provided contains the
// required auth parameters for a worker and they are valid.
func (a *API) authWorkerFromRequest(r *http.Request) (common.Address, *Error) {
	// Extract the sequencer UUID from the url and compare with UUID of the
	// current sequencer API.
	sequencerUUID := chi.URLParam(r, "uuid")
	if a.sequencerUUID.String() != sequencerUUID {
		return common.Address{}, &ErrResourceNotFound
	}

	// Extract the address from query params
	strWorkerAddress := r.URL.Query().Get(WorkerAddressQueryParam)
	if strWorkerAddress == "" {
		err := ErrMalformedWorkerInfo.Withf("missing address")
		return common.Address{}, &err
	}

	// Extract the signature from query params
	strToken := r.URL.Query().Get(WorkerTokenQueryParam)
	if strToken == "" {
		err := ErrMalformedWorkerInfo.Withf("missing signature")
		return common.Address{}, &err
	}

	// Check if the signature is valid and the token is not expired
	valid, timestamp, err := workers.VerifyWorkerHexToken(strToken, strWorkerAddress, a.sequencerSigner.Address())
	if err != nil {
		err := ErrMalformedWorkerInfo.WithErr(err)
		return common.Address{}, &err
	} else if !valid {
		err := ErrInvalidWorkerAuthtoken.Withf("invalid signature")
		return common.Address{}, &err
	} else if time.Since(timestamp) > a.workersAuthtokenExpiration {
		err := ErrExpiredWorkerAuthtoken.Withf("generate a new one to continue")
		return common.Address{}, &err
	}

	// Extract name from query params
	workerName := r.URL.Query().Get(WorkerNameQueryParam)
	if workerName == "" {
		var err error
		workerName, err = workers.WorkerNameFromAddress(strWorkerAddress)
		if err != nil {
			err := ErrMalformedWorkerInfo.WithErr(err)
			return common.Address{}, &err
		}
	}
	// Add the worker to the WorkerManager, if it already exists it will be
	// updated
	a.jobsManager.WorkerManager.AddWorker(strWorkerAddress, workerName)
	return common.HexToAddress(strWorkerAddress), nil
}

// workersList handles GET /workers
func (a *API) workersList(w http.ResponseWriter, r *http.Request) {
	// Check if workers are configured
	if a.jobsManager == nil {
		httpWriteJSON(w, WorkersListResponse{
			Workers: []WorkerInfo{},
		})
		return
	}

	// Get all worker statistics
	workerStats, err := a.jobsManager.WorkerManager.ListWorkerStats()
	if err != nil {
		log.Warnw("failed to get worker statistics",
			"error", err.Error())
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}
	apiWorkers := make([]WorkerInfo, len(workerStats))
	for i, worker := range workerStats {
		apiWorkers[i] = WorkerInfo{
			// omit the address to prevent exposing it
			Name:         worker.Name,
			SuccessCount: worker.SuccessCount,
			FailedCount:  worker.FailedCount,
		}
	}
	httpWriteJSON(w, WorkersListResponse{
		Workers: apiWorkers,
	})
}

// workersTokenData handles GET /workers/authTokenData that response with a
// text that contains the signature of the master message. This message is
// used to setup new workers. The text follows this format:
//
//	Authorizing worker in sequencer '<sequencerAddress>' at <timestamp>
func (a *API) workersTokenData(w http.ResponseWriter, r *http.Request) {
	// sign message with api.workerSigner
	timestamp := time.Now()
	msg, createdAt, authTokenSuffix := workers.WorkerAuthTokenData(a.sequencerSigner.Address(), timestamp)
	signature, err := a.sequencerSigner.Sign([]byte(msg))
	if err != nil {
		log.Warnw("failed to sign worker message",
			"error", err.Error())
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}
	// return the response
	httpWriteJSON(w, WorkerAuthDataResponse{
		Message:         msg,
		Signature:       signature.Bytes(),
		CreatedAt:       createdAt,
		AuthTokenSuffix: authTokenSuffix,
	})
}

// workersNewJob handles GET /workers/{uuid}/job
func (a *API) workersNewJob(w http.ResponseWriter, r *http.Request) {
	workerAddr, apiErr := a.authWorkerFromRequest(r)
	if apiErr != nil {
		log.Warnw("failed to verify worker signature", "error", apiErr.Error())
		apiErr.Write(w)
		return
	}

	// Check if worker is available
	if available, err := a.jobsManager.IsWorkerAvailable(workerAddr.Hex()); !available {
		log.Warnw("worker not available", "worker", workerAddr.Hex())
		ErrWorkerNotAvailable.WithErr(err).Write(w)
		return
	}

	// Get next ballot
	ballot, voteID, err := a.storage.NextBallot()
	if err != nil {
		if errors.Is(err, storage.ErrNoMoreElements) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Warnw("failed to get next ballot for worker",
			"error", err.Error(),
			"worker", workerAddr.Hex())
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	// Track the job
	voteIDStr := hex.EncodeToString(voteID)
	if _, err := a.jobsManager.RegisterJob(workerAddr.Hex(), voteID); err != nil {
		log.Warnw("no available workers for job",
			"voteID", voteIDStr,
			"worker", workerAddr.Hex(),
			"error", err.Error())
		ErrGenericInternalServerError.Withf("no available workers for job").Write(w)
		return
	}

	log.Infow("assigned job to worker",
		"voteID", voteIDStr,
		"worker", workerAddr.Hex(),
		"processID", hex.EncodeToString(ballot.ProcessID))

	// Return ballot
	data, err := storage.EncodeArtifact(ballot)
	if err != nil {
		log.Warnw("failed to encode ballot for worker",
			"error", err.Error(),
			"voteID", voteIDStr,
		)
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	httpWriteBinary(w, data)
}

// workersSubmitJob handles POST /workers/{uuid}/job
func (a *API) workersSubmitJob(w http.ResponseWriter, r *http.Request) {
	// Check worker signature
	workerAddr, apiErr := a.authWorkerFromRequest(r)
	if apiErr != nil {
		log.Warnw("failed to verify worker signature", "error", apiErr.Error())
		apiErr.Write(w)
		return
	}

	// Decode verified ballot
	var workerVerifiedBallot storage.VerifiedBallot
	body, err := io.ReadAll(r.Body) // Read the body to ensure it's consumed
	if err != nil {
		log.Warnw("failed to read request body",
			"error", err.Error())
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}
	if err := storage.DecodeArtifact(body, &workerVerifiedBallot); err != nil {
		log.Warnw("failed to decode verified ballot",
			"error", err.Error())
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}

	// Sanity checks
	if workerVerifiedBallot.VoteID == nil || workerVerifiedBallot.Proof == nil {
		log.Warnw("malformed verified ballot", "error", "missing required fields")
		ErrMalformedBody.Withf("missing required fields").Write(w)
		return
	}

	// Check if the job exists and is assigned to this worker
	if _, err := a.jobsManager.Job(workerAddr.Hex(), workerVerifiedBallot.VoteID); err != nil {
		log.Warnw("failed to get job for worker",
			"error", err.Error(),
			"worker", workerAddr.Hex())
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	// Get the original ballot from the storage, from now on we will use it to
	// validate the verified ballot
	ballot, err := a.storage.Ballot(workerVerifiedBallot.VoteID)
	if err != nil {
		log.Warnw("failed to get ballot for voteID",
			"error", err.Error(),
			"voteID", workerVerifiedBallot.VoteID.String())
		ErrResourceNotFound.Withf("ballot not found").Write(w)
		return
	}

	// Prepare the verified ballot to be used and stored
	// Note that we use the original ballot parameters, not the ones
	// provided by the worker, to avoid tampering.
	verifiedBallot := storage.VerifiedBallot{
		VoteID:          workerVerifiedBallot.VoteID,
		Proof:           workerVerifiedBallot.Proof,
		ProcessID:       ballot.ProcessID,
		Address:         ballot.Address,
		EncryptedBallot: ballot.EncryptedBallot,
		InputsHash:      ballot.BallotInputsHash,
		VoterWeight:     ballot.VoterWeight,
	}

	// Verify the worker proof
	if err := voteverifier.PublicInputs(verifiedBallot.InputsHash).VerifyProof(groth16.Proof(verifiedBallot.Proof)); err != nil {
		log.Warnw("failed to verify public circuit inputs", "error", err.Error())
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	// Set job as completed
	job := a.jobsManager.CompleteJob(verifiedBallot.VoteID, true)
	if job == nil {
		log.Warnw("job not found or expired",
			"voteID", verifiedBallot.VoteID.String())
		ErrResourceNotFound.Withf("job not found or expired").Write(w)
		return
	}

	// Mark ballot as done
	if err := a.storage.MarkBallotDone(verifiedBallot.VoteID, &verifiedBallot); err != nil {
		log.Warnw("failed to mark ballot as done",
			"error", err.Error(),
			"voteID", verifiedBallot.VoteID.String())
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	stats, err := a.jobsManager.WorkerManager.WorkerStats(job.Address)
	if err != nil {
		log.Warnw("failed to get worker job count",
			"error", err.Error(),
			"worker", job.Address)
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	log.Infow("worker job completed",
		"voteID", verifiedBallot.VoteID.String(),
		"workerAddr", job.Address,
		"workerName", stats.Name,
		"duration", time.Since(job.Timestamp).String(),
		"successCount", stats.SuccessCount,
		"failedCount", stats.FailedCount,
	)

	// Prepare response
	response := WorkerJobResponse{
		VoteID:       verifiedBallot.VoteID,
		Address:      job.Address,
		SuccessCount: stats.SuccessCount,
		FailedCount:  stats.FailedCount,
	}

	httpWriteJSON(w, response)
}
