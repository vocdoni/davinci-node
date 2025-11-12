package api

import (
	"fmt"
	"net/url"
	"strings"
)

// Route constants for the API endpoints

const (
	// Health endpoints
	PingEndpoint = "/ping" // Health check endpoint

	// Process endpoints
	ProcessURLParam           = "processId"                                                                   // URL parameter for process ID
	AddressURLParam           = "address"                                                                     // URL parameter for address                                                                    // URL parameter for participant address
	ProcessesEndpoint         = "/processes"                                                                  // GET: List processes, POST: Create process
	ProcessEndpoint           = "/processes/{" + ProcessURLParam + "}"                                        // GET: Get process info
	CensusParticipantEndpoint = "/processes/{" + ProcessURLParam + "}/participants/{" + AddressURLParam + "}" // GET: Get participant info for a process

	// Vote endpoints
	VotesEndpoint = "/votes" // POST: Submit a vote

	// Vote status endpoints
	VoteIDURLParam        = "voteId"                                                                       // URL parameter for vote ID
	VoteStatusEndpoint    = VotesEndpoint + "/{" + ProcessURLParam + "}/voteId/{" + VoteIDURLParam + "}"   // GET: Check vote status
	VoteByAddressEndpoint = VotesEndpoint + "/{" + ProcessURLParam + "}/address/{" + AddressURLParam + "}" // GET: Get vote by address

	// Info endpoint
	InfoEndpoint = "/info" // GET: Get ballot proof information

	// Host load endpoint
	HostLoadEndpoint = "/info/load" // GET: Get host load metrics

	// Static file serving endpoint
	StaticFilesEndpoint = "/app*" // GET: Serve static files from the /webapp directory

	// Worker URL params and endpoints
	SequencerUUIDURLParam   = "uuid"    // Param for worker UUID
	WorkerAddressQueryParam = "address" // URL query param for worker address
	WorkerNameQueryParam    = "name"    // URL query param for worker name
	WorkerTokenQueryParam   = "token"   // URL query param for worker token

	WorkersEndpoint         = "/workers/{" + SequencerUUIDURLParam + "}" // Base workers endpoint
	WorkerTokenDataEndpoint = WorkersEndpoint + "/authData"              // GET: Message to be signed by workers
	WorkerJobEndpoint       = WorkersEndpoint + "/job"                   // GET: New job for worker POST: Submit job from worker

	// Sequencer endpoints
	SequencerWorkersEndpoint = "/sequencer/workers" // GET: List worker statistics

	// Metadata endpoints
	MetadataHashParam   = "metadataHash"                                       // URL parameter for metadata hash
	MetadataSetEndpoint = "/metadata"                                          // POST: Set metadata
	MetadataGetEndpoint = MetadataSetEndpoint + "/{" + MetadataHashParam + "}" // GET: Get metadata
)

// EndpointWithParam creates an endpoint URL by replacing the parameter
// placeholder with the actual value. Used to build fully qualified
// endpoint URLs.
func EndpointWithParam(path, key, param string) string {
	rawKey := fmt.Sprintf("{%s}", key)

	// Always try to replace the placeholder, even if it's after the '?'
	if strings.Contains(path, rawKey) {
		return strings.Replace(path, rawKey, url.PathEscape(param), 1)
	}

	// Fallback: add as query param
	escapedKey := url.QueryEscape(key)
	escapedVal := url.QueryEscape(param)

	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}

	return fmt.Sprintf("%s%s%s=%s", path, sep, escapedKey, escapedVal)
}

// LogExcludedPrefixes defines URL prefixes to exclude from request logging
var LogExcludedPrefixes = []string{
	PingEndpoint,
	WorkersEndpoint,
	InfoEndpoint,
}
