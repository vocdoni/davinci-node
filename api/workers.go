package api

import (
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/workers"
)

// startWorkerTimeoutMonitor starts the timeout monitor for worker jobs
func (a *API) startWorkerTimeoutMonitor() {
	a.jobsManager = workers.NewJobsManager(a.storage, a.workerTimeout, a.banRules)
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

// workerGetJob handles GET /workers/{uuid}/{workerAddress}?name={workerName}
func (a *API) workerGetJob(w http.ResponseWriter, r *http.Request) {
	// Extract UUID and address from URL params
	uuid := chi.URLParam(r, WorkerUUIDParam)
	workerAddress := chi.URLParam(r, WorkerAddressParam)

	// Validate UUID
	if uuid != a.workerUUID.String() {
		ErrUnauthorized.Write(w)
		return
	}
	// Validate worker address
	if _, err := workers.ValidWorkerAddress(workerAddress); err != nil {
		ErrMalformedWorkerInfo.Withf("invalid worker address").Write(w)
		return
	}

	// If the worker is not registered, add it
	if _, ok := a.jobsManager.WorkerManager.GetWorker(workerAddress); !ok {
		// Extract worker name from query parameter
		workerName := r.URL.Query().Get("name")
		// Validate worker name, if not provided, try to derive it from the
		// address
		if workerName == "" {
			var err error
			workerName, err = workers.WorkerNameFromAddress(workerAddress)
			if err != nil {
				ErrMalformedWorkerInfo.WithErr(err).Write(w)
				return
			}
		}
		// Add worker to the manager with its name
		a.jobsManager.WorkerManager.AddWorker(workerAddress, workerName)
	}

	// Check if worker is available
	if available, err := a.jobsManager.IsWorkerAvailable(workerAddress); !available {
		log.Warnw("worker not available",
			"worker", workerAddress,
			"uuid", uuid)
		ErrWorkerNotAvailable.WithErr(err).Write(w)
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
	if _, err := a.jobsManager.RegisterJob(workerAddress, key); err != nil {
		log.Warnw("no available workers for job",
			"voteID", voteIDStr,
			"worker", workerAddress,
			"error", err.Error())
		ErrGenericInternalServerError.Withf("no available workers for job").Write(w)
		return
	}

	log.Infow("assigned job to worker",
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

	// Set job as completed
	job := a.jobsManager.CompleteJob(vb.VoteID, true)
	if job == nil {
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

	stats, err := a.jobsManager.WorkerManager.WorkerStats(job.Address)
	if err != nil {
		log.Warnw("failed to get worker job count",
			"error", err.Error(),
			"worker", job.Address)
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	log.Infow("worker job completed",
		"voteID", voteIDStr,
		"workerAddr", job.Address,
		"workerName", stats.Name,
		"duration", time.Since(job.Timestamp).String(),
		"successCount", stats.SuccessCount,
		"failedCount", stats.FailedCount,
	)

	// Prepare response
	response := WorkerJobResponse{
		VoteID:       vb.VoteID,
		Address:      job.Address,
		SuccessCount: stats.SuccessCount,
		FailedCount:  stats.FailedCount,
	}

	httpWriteJSON(w, response)
}

// workersList handles GET /workers
func (a *API) workersList(w http.ResponseWriter, r *http.Request) {
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

	response := WorkersListResponse{
		Workers: apiWorkers,
	}

	httpWriteJSON(w, response)
}
