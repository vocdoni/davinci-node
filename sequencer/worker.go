package sequencer

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vocdoni/davinci-node/api"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// ErrNoJobAvailable is returned when there are no jobs available for the worker to process.
// This can happen if the master node has no ballots assigned to this worker or if all ballots have been processed.
var ErrNoJobAvailable = errors.New("no job available")

// NewWorker creates a Sequencer instance configured for worker mode.
// Only loads the necessary artifacts for ballot processing (vote verifier).
//
// Parameters:
//   - stg: Storage instance for accessing ballots and other data
//   - masterURL: URL of the master node to connect to
//   - workerAddress: Ethereum address identifying this worker
//
// Returns a configured Sequencer instance for worker mode or an error if initialization fails.
func NewWorker(stg *storage.Storage, masterURL string, workerAddress string) (*Sequencer, error) {
	if stg == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}
	if masterURL == "" {
		return nil, fmt.Errorf("masterURL cannot be empty for worker mode")
	}
	if workerAddress == "" {
		return nil, fmt.Errorf("workerAddress must be provided")
	}

	startTime := time.Now()

	s := &Sequencer{
		stg:             stg,
		contracts:       nil,               // Workers don't need web3 contracts
		batchTimeWindow: 0,                 // Workers don't use batch processing
		pids:            NewProcessIDMap(), // Still needed for ExistsProcessID check
		prover:          DefaultProver,
		masterURL:       masterURL,
		workerAddress:   workerAddress,
	}

	s.internalCircuits = new(internalCircuits)
	s.bVkCircom = ballotprooftest.TestCircomVerificationKey

	log.Debugw("reading ccs and pk cicuit artifact", "circuit", "voteVerifier")
	var err error
	s.vvCcs, s.vvPk, err = loadCircuitArtifacts(voteverifier.Artifacts)
	if err != nil {
		return nil, fmt.Errorf("failed to load vote verifier artifacts: %w", err)
	}

	log.Debugw("worker sequencer initialized",
		"masterURL", masterURL,
		"workerAddress", workerAddress,
		"took", time.Since(startTime).String(),
	)

	return s, nil
}

// startWorkerProcessor starts the worker goroutine that fetches and processes jobs
func (s *Sequencer) startWorkerProcessor() error {
	go func() {
		ticker := time.NewTicker(time.Second * 5)
		defer ticker.Stop()

		consecutiveErrors := 0
		maxConsecutiveErrors := 10

		log.Infow("worker processor started")

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("worker processor stopped")
				return
			case <-ticker.C:
				if err := s.processWorkerJob(); err != nil {
					if errors.Is(err, ErrNoJobAvailable) {
						consecutiveErrors = 0
						continue
					}

					consecutiveErrors++
					log.Warnw("failed to process worker job",
						"error", err.Error(),
						"consecutiveErrors", consecutiveErrors)

					if consecutiveErrors >= maxConsecutiveErrors {
						log.Errorw(nil, "too many consecutive errors, backing off")
						time.Sleep(30 * time.Second)
						consecutiveErrors = 0
					}
				} else {
					consecutiveErrors = 0
				}
			}
		}
	}()
	return nil
}

// processWorkerJob fetches a job from master, processes it, and returns the result
func (s *Sequencer) processWorkerJob() error {
	// GET job from master
	ballot, err := s.fetchJobFromMaster()
	if err != nil {
		return err
	}

	log.Debugw("processing worker job", "voteID", fmt.Sprintf("%x", ballot.VoteID))

	// Ensure the Process exists in local storage - fetch from master if needed
	pid := new(types.ProcessID)
	if err := pid.Unmarshal(ballot.ProcessID); err != nil {
		return fmt.Errorf("failed to unmarshal process ID: %w", err)
	}

	// Check if process exists locally
	_, err = s.stg.Process(pid)
	if err != nil {
		log.Debugw("process not found locally, fetching from master",
			"processID", fmt.Sprintf("%x", ballot.ProcessID))

		// Fetch process from master
		if err := s.fetchProcessFromMaster(pid); err != nil {
			return fmt.Errorf("failed to fetch process from master: %w", err)
		}
	}

	// Process the ballot using existing logic
	verifiedBallot, err := s.processBallot(ballot)
	if err != nil {
		log.Warnw("failed to process ballot in worker mode",
			"error", err.Error(),
			"voteID", fmt.Sprintf("%x", ballot.VoteID))
		return fmt.Errorf("failed to process ballot: %w", err)
	}

	// POST result back to master
	return s.submitJobToMaster(verifiedBallot)
}

// fetchProcessFromMaster fetches process information from the master and stores it locally
func (s *Sequencer) fetchProcessFromMaster(pid *types.ProcessID) error {
	// Extract base URL from masterURL (remove the workers path)
	baseURL := s.masterURL
	if strings.Contains(baseURL, "/workers/") {
		parts := strings.Split(baseURL, "/workers/")
		if len(parts) > 0 {
			baseURL = parts[0]
		}
	}

	// Construct process endpoint URL using the defined API routes
	processIDHex := hex.EncodeToString(pid.Marshal())
	url := baseURL + api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, processIDHex)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch process: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to fetch process, status %d: %s", resp.StatusCode, body)
	}

	// Decode the process
	var process types.Process
	if err := json.NewDecoder(resp.Body).Decode(&process); err != nil {
		return fmt.Errorf("failed to decode process: %w", err)
	}

	// Store the process locally
	if err := s.stg.NewProcess(&process); err != nil {
		return fmt.Errorf("failed to store process locally: %w", err)
	}

	log.Debugw("fetched and stored process from master",
		"processID", processIDHex,
		"ballotMode", process.BallotMode.String())

	return nil
}

// fetchJobFromMaster performs GET request to master
func (s *Sequencer) fetchJobFromMaster() (*storage.Ballot, error) {
	url := fmt.Sprintf("%s/%s", s.masterURL, s.workerAddress)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, ErrNoJobAvailable
	case http.StatusOK:
		var ballot storage.Ballot
		data, err := io.ReadAll(resp.Body) // Read the body to ensure it's consumed
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		if err := storage.DecodeArtifact(data, &ballot); err != nil {
			return nil, fmt.Errorf("failed to decode ballot: %w", err)
		}

		// Register the process ID locally for ExistsProcessID check
		s.AddProcessID(ballot.ProcessID)

		log.Debugw("fetched job from master",
			"voteID", fmt.Sprintf("%x", ballot.VoteID),
			"processID", fmt.Sprintf("%x", ballot.ProcessID))

		return &ballot, nil
	case http.StatusForbidden:
		return nil, fmt.Errorf("forbidden: invalid worker authentication")
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, body)
	}
}

// submitJobToMaster performs POST request to master with verified ballot
func (s *Sequencer) submitJobToMaster(vb *storage.VerifiedBallot) error {
	url := s.masterURL // POST doesn't need address in URL

	body, err := storage.EncodeArtifact(vb)
	if err != nil {
		return fmt.Errorf("failed to marshal verified ballot: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(url, "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to submit job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to submit job, status %d: %s", resp.StatusCode, body)
	}

	// Read the response body
	workerResponse := &api.WorkerJobResponse{}
	if err := json.NewDecoder(resp.Body).Decode(workerResponse); err != nil {
		return fmt.Errorf("failed to decode worker response: %w", err)
	}

	log.Infow("submitted job to master",
		"voteID", fmt.Sprintf("%x", vb.VoteID),
		"processID", fmt.Sprintf("%x", vb.ProcessID),
		"success", workerResponse.SuccessCount,
		"failed", workerResponse.FailedCount,
	)

	return nil
}
