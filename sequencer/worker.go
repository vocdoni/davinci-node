package sequencer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/vocdoni/davinci-node/api"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/workers"
)

// ErrNoJobAvailable is returned when there are no jobs available for the
// worker to process. This can happen if the sequencer node has no ballots
// assigned to this worker or if all ballots have been processed.
var (
	ErrNoJobAvailable = errors.New("no job available")
	ErrWorkerBanned   = errors.New("worker is banned")
	ErrWorkerBusy     = errors.New("worker is busy")

	CooldownDuration = 30 * time.Second // Default cooldown duration when worker is busy or banned
)

// NewWorker creates a Sequencer instance configured for worker mode.
// Only loads the necessary artifacts for ballot processing (vote verifier).
//
// Parameters:
//   - stg: Storage instance for accessing ballots and other data
//   - sequencerURL: URL of the sequencer node to connect to
//   - sequencerUUID: UUID of the sequencer node to connect to
//   - workerAddr: Ethereum address identifying this worker
//   - workerToken: Hex-encoded authentication token for the worker
//   - workerName: Name of the worker (optional)
//
// Returns a configured Sequencer instance for worker mode or an error if
// initialization fails.
func NewWorker(stg *storage.Storage, rawSequencerURL, workerAddr, workerToken, workerName string) (*Sequencer, error) {
	if stg == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}
	// parse sequencer URL and UUID from the raw URL provided
	sequencerURL, sequencerUUID, err := parseSequencerURL(rawSequencerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sequencer URL: %w", err)
	}
	// check if sequencer URL is provided
	if sequencerURL == "" {
		return nil, fmt.Errorf("sequencerURL cannot be empty for worker mode")
	}
	// check if sequencer UUID is provided
	if sequencerUUID == "" {
		return nil, fmt.Errorf("sequencerUUID cannot be empty for worker mode")
	}
	// check if a valid worker address is provided
	wAddr, err := workers.ValidWorkerAddress(workerAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid worker address: %w", err)
	}
	// if no worker name is provided, generate it from the address
	if workerName == "" {
		workerName, err = workers.WorkerNameFromAddress(wAddr.String())
		if err != nil {
			return nil, fmt.Errorf("failed to generate worker name from address: %w", err)
		}
	}
	// check if a valid hex signature is provided
	if workerToken == "" {
		return nil, fmt.Errorf("hexSignature cannot be empty for worker mode")
	}

	startTime := time.Now()
	s := &Sequencer{
		stg:             stg,
		contracts:       nil,               // Workers don't need web3 contracts
		batchTimeWindow: 0,                 // Workers don't use batch processing
		pids:            NewProcessIDMap(), // Still needed for ExistsProcessID check
		prover:          prover.DefaultProver,
		sequencerURL:    sequencerURL,
		sequencerUUID:   sequencerUUID,
		workerAddress:   wAddr,
		workerName:      workerName,
		workerAuthtoken: workerToken,
	}

	s.internalCircuits = new(internalCircuits)
	s.bVkCircom = ballotprooftest.TestCircomVerificationKey

	log.Debugw("reading ccs and pk cicuit artifact", "circuit", "voteVerifier")
	s.vvCcs, s.vvPk, err = loadCircuitArtifacts(voteverifier.Artifacts)
	if err != nil {
		return nil, fmt.Errorf("failed to load vote verifier artifacts: %w", err)
	}

	log.Debugw("worker sequencer initialized",
		"sequencerURL", sequencerURL,
		"workerAddress", workerAddr,
		"workerName", workerName,
		"took", time.Since(startTime).String(),
	)

	return s, nil
}

// parseSequencerURL extracts the base URL and UUID from the sequencerURL of
// the worker. It returns an error if the sequencerURL is not set or if it
// does not contain the expected format with "/workers/" path.
func parseSequencerURL(rawURL string) (string, string, error) {
	if rawURL == "" {
		return "", "", fmt.Errorf("sequencer URL is not set for worker mode")
	}
	// check if raw url matches and the UUID is captured
	// it should capture only the uuid
	uuidPattern := `([^/?#]+)(?:/[^?#]*)?(?:\?[^#]*)?$`
	uriNeedle := fmt.Sprintf("{%s}", api.SequencerUUIDURLParam)
	uuidTemplate := strings.ReplaceAll(api.WorkersEndpoint, uriNeedle, uuidPattern)
	sequencerUUIDRgx := regexp.MustCompile(uuidTemplate)
	matches := sequencerUUIDRgx.FindStringSubmatch(rawURL)
	if len(matches) != 2 {
		return "", "", fmt.Errorf("the url has not UUID: %s", rawURL)
	}
	uuid := matches[1]
	// split the raw URL by the needle and take the first part as the base path
	splitter := api.EndpointWithParam(api.WorkersEndpoint, api.SequencerUUIDURLParam, uuid)
	basePath := strings.Split(rawURL, splitter)[0]
	return basePath, uuid, nil
}

// startWorkerModeServices starts the worker goroutine that fetches and processes jobs
func (s *Sequencer) startWorkerModeServices() error {
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
					if errors.Is(err, ErrWorkerBanned) || errors.Is(err, ErrWorkerBusy) {
						time.Sleep(CooldownDuration)
						continue
					}

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
						time.Sleep(CooldownDuration)
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

	// Check if process exists locally
	_, err = s.stg.Process(ballot.ProcessID)
	if err != nil {
		log.Debugw("process not found locally, fetching from master",
			"processID", fmt.Sprintf("%x", ballot.ProcessID))

		// Fetch process from master
		if err := s.fetchProcessFromMaster(ballot.ProcessID); err != nil {
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
func (s *Sequencer) fetchProcessFromMaster(processID types.ProcessID) error {
	// Construct process endpoint URL using the defined API routes
	uri := api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, processID.String())
	seqUrl := fmt.Sprintf("%s%s", s.sequencerURL, uri)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(seqUrl)
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
		"processID", processID.String(),
		"ballotMode", process.BallotMode.String())

	return nil
}

// fetchJobFromMaster performs GET request to master
func (s *Sequencer) fetchJobFromMaster() (*storage.Ballot, error) {
	uri := api.EndpointWithParam(api.WorkerJobEndpoint, api.SequencerUUIDURLParam, s.sequencerUUID)
	uri = api.EndpointWithParam(uri, api.WorkerAddressQueryParam, s.workerAddress.String())
	uri = api.EndpointWithParam(uri, api.WorkerTokenQueryParam, s.workerAuthtoken)
	uri = api.EndpointWithParam(uri, api.WorkerNameQueryParam, s.workerName)
	seqUrl := fmt.Sprintf("%s%s", s.sequencerURL, uri)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(seqUrl)
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
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("unauthorized: invalid worker authentication")
	case http.StatusForbidden:
		return nil, fmt.Errorf("forbidden: worker is banned")
	default:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read error response body: %w", err)
		}
		originalErr := &api.Error{}
		if jsonErr := json.Unmarshal(body, originalErr); jsonErr != nil {
			log.Debugw("failed to unmarshal error response", "error", jsonErr.Error())
		}
		if originalErr.Code == api.ErrWorkerNotAvailable.Code {
			return nil, ErrNoJobAvailable
		}
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, originalErr.Err)
	}
}

// submitJobToMaster performs POST request to master with verified ballot
func (s *Sequencer) submitJobToMaster(vb *storage.VerifiedBallot) error {
	uri := api.EndpointWithParam(api.WorkerJobEndpoint, api.SequencerUUIDURLParam, s.sequencerUUID)
	uri = api.EndpointWithParam(uri, api.WorkerAddressQueryParam, s.workerAddress.String())
	uri = api.EndpointWithParam(uri, api.WorkerNameQueryParam, s.workerName)
	uri = api.EndpointWithParam(uri, api.WorkerTokenQueryParam, s.workerAuthtoken)
	uri = api.EndpointWithParam(uri, api.WorkerNameQueryParam, s.workerName)
	seqUrl := fmt.Sprintf("%s%s", s.sequencerURL, uri)

	body, err := storage.EncodeArtifact(vb)
	if err != nil {
		return fmt.Errorf("failed to marshal verified ballot: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(seqUrl, "application/octet-stream", bytes.NewReader(body))
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
