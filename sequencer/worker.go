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

	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

var ErrNoJobAvailable = errors.New("no job available")

// startWorkerProcessor starts the worker goroutine that fetches and processes jobs
func (s *Sequencer) startWorkerProcessor() error {
	go func() {
		ticker := time.NewTicker(time.Second)
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

	log.Debugw("processing worker job", "voteID", fmt.Sprintf("%x", ballot.VoteID()))

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
			"voteID", fmt.Sprintf("%x", ballot.VoteID()))
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
	if err := s.stg.SetProcess(&process); err != nil {
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

	client := &http.Client{Timeout: 30 * time.Second}
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
		if err := json.NewDecoder(resp.Body).Decode(&ballot); err != nil {
			return nil, fmt.Errorf("failed to decode ballot: %w", err)
		}

		// Register the process ID locally for ExistsProcessID check
		s.AddProcessID(ballot.ProcessID)

		log.Debugw("fetched job from master",
			"voteID", fmt.Sprintf("%x", ballot.VoteID()),
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

	body, err := json.Marshal(vb)
	if err != nil {
		return fmt.Errorf("failed to marshal verified ballot: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to submit job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to submit job, status %d: %s", resp.StatusCode, body)
	}

	log.Debugw("submitted job to master",
		"voteID", fmt.Sprintf("%x", vb.VoteID))

	return nil
}
