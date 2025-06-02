package api

import (
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
)

// startWorkerTimeoutMonitor starts the timeout monitor for worker jobs
func (a *API) startWorkerTimeoutMonitor() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			a.checkWorkerTimeouts()
		}
	}()
}

// workerGetJob handles GET /workers/{uuid}/{address}
func (a *API) workerGetJob(w http.ResponseWriter, r *http.Request) {
	// Extract UUID and address from URL params
	uuid := chi.URLParam(r, WorkerUUIDParam)
	workerAddress := chi.URLParam(r, WorkerAddressParam)

	// Validate UUID
	if uuid != a.workerUUID.String() {
		ErrUnauthorized.Write(w)
		return
	}

	// Get next ballot
	ballot, key, err := a.storage.NextBallot()
	if err != nil {
		if errors.Is(err, storage.ErrNoMoreElements) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Warnw("failed to get next ballot for worker",
			"error", err.Error(),
			"worker", workerAddress)
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	// Track the job
	voteIDStr := hex.EncodeToString(key)
	a.jobsMutex.Lock()
	a.activeJobs[voteIDStr] = &workerJob{
		VoteID:    key,
		Address:   workerAddress,
		Timestamp: time.Now(),
	}
	a.jobsMutex.Unlock()

	log.Debugw("assigned job to worker",
		"voteID", voteIDStr,
		"worker", workerAddress,
		"processID", hex.EncodeToString(ballot.ProcessID))

	// Check if worker is registered for this process
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

// workerSubmitJob handles POST /workers/{uuid}
func (a *API) workerSubmitJob(w http.ResponseWriter, r *http.Request) {
	// Extract UUID from URL param
	uuid := chi.URLParam(r, WorkerUUIDParam)

	// Validate UUID
	if uuid != a.workerUUID.String() {
		ErrUnauthorized.Write(w)
		return
	}

	// Decode verified ballot
	var vb storage.VerifiedBallot
	body, err := io.ReadAll(r.Body) // Read the body to ensure it's consumed
	if err != nil {
		log.Warnw("failed to read request body",
			"error", err.Error())
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}
	if err := storage.DecodeArtifact(body, &vb); err != nil {
		log.Warnw("failed to decode verified ballot",
			"error", err.Error())
		ErrMalformedBody.WithErr(err).Write(w)
		return
	}

	// Validate job ownership
	voteIDStr := hex.EncodeToString(vb.VoteID)

	a.jobsMutex.RLock()
	job, exists := a.activeJobs[voteIDStr]
	a.jobsMutex.RUnlock()

	if !exists {
		log.Warnw("job not found or expired",
			"voteID", voteIDStr)
		ErrResourceNotFound.Withf("job not found or expired").Write(w)
		return
	}

	// Mark ballot as done
	if err := a.storage.MarkBallotDone(vb.VoteID, &vb); err != nil {
		log.Warnw("failed to mark ballot as done",
			"error", err.Error(),
			"voteID", voteIDStr)
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	// Update worker stats
	if err := a.storage.IncreaseWorkerJobCount(job.Address, 1); err != nil {
		log.Warnw("failed to update worker success stats",
			"error", err.Error(),
			"worker", job.Address)
	}

	success, failed, err := a.storage.WorkerJobCount(job.Address)
	if err != nil {
		log.Warnw("failed to get worker job count",
			"error", err.Error(),
			"worker", job.Address)
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	// Remove from active jobs
	a.jobsMutex.Lock()
	delete(a.activeJobs, voteIDStr)
	a.jobsMutex.Unlock()

	log.Debugw("worker job completed",
		"voteID", voteIDStr,
		"worker", job.Address,
		"duration", time.Since(job.Timestamp).String(),
		"successCount", success,
		"failedCount", failed,
	)

	// Prepare response
	response := WorkerJobResponse{
		VoteID:       vb.VoteID,
		Address:      job.Address,
		SuccessCount: success,
		FailedCount:  failed,
	}

	httpWriteJSON(w, response)
}

// checkWorkerTimeouts removes timed out jobs and their reservations
func (a *API) checkWorkerTimeouts() {
	now := time.Now()
	var timedOutJobs []*workerJob

	a.jobsMutex.Lock()
	for voteID, job := range a.activeJobs {
		if now.Sub(job.Timestamp) > a.workerTimeout {
			timedOutJobs = append(timedOutJobs, job)
			delete(a.activeJobs, voteID)
		}
	}
	a.jobsMutex.Unlock()

	// Process timeouts
	for _, job := range timedOutJobs {
		voteIDStr := hex.EncodeToString(job.VoteID)
		log.Warnw("job timeout",
			"voteID", voteIDStr,
			"worker", job.Address,
			"duration", now.Sub(job.Timestamp).String())

		// Remove ballot reservation (this will put it back in the queue)
		if err := a.storage.RemoveBallot(nil, job.VoteID); err != nil {
			log.Warnw("failed to remove timed out ballot",
				"error", err.Error(),
				"voteID", voteIDStr)
		}

		// Update worker failed count
		if err := a.storage.IncreaseWorkerFailedJobCount(job.Address, 1); err != nil {
			log.Warnw("failed to update worker failed stats",
				"error", err.Error(),
				"worker", job.Address)
		}
	}

	if len(timedOutJobs) > 0 {
		log.Infow("processed job timeouts",
			"count", len(timedOutJobs))
	}
}
