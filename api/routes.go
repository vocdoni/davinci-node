package api

import "strings"

// Route constants for the API endpoints

const (
	// Health endpoints
	PingEndpoint = "/ping" // Health check endpoint

	// Process endpoints
	ProcessesEndpoint = "/processes"                           // GET: List processes, POST: Create process
	ProcessURLParam   = "processId"                            // URL parameter for process ID
	ProcessEndpoint   = "/processes/{" + ProcessURLParam + "}" // GET: Get process info

	// Test process endpoints - for testing only
	TestSetProcessEndpoint = "/processes/test"                           // Store process info for testing
	TestProcessEndpoint    = "/processes/test/{" + ProcessURLParam + "}" // Get test process info

	// Vote endpoints
	VotesEndpoint = "/votes" // POST: Submit a vote

	// Vote status endpoints
	VoteStatusVoteIDParam = "voteId"                                                                            // URL parameter for vote ID
	VoteStatusEndpoint    = VotesEndpoint + "/{" + ProcessURLParam + "}/voteId/{" + VoteStatusVoteIDParam + "}" // GET: Check vote status

	// Vote nullifier endpoint
	VoteByNullifierNullifierParam = "nullifier"                                                                                    // URL parameter for nullifier
	VoteByNullifierEndpoint       = VotesEndpoint + "/{" + ProcessURLParam + "}/nullifier/{" + VoteByNullifierNullifierParam + "}" // GET: Get vote by nullifier

	// Vote address check endpoint
	VoteCheckAddressParam = "address"                                                                            // URL parameter for address
	VoteCheckEndpoint     = VotesEndpoint + "/{" + ProcessURLParam + "}/address/{" + VoteCheckAddressParam + "}" // GET: Check if address voted

	// Info endpoint
	InfoEndpoint = "/info" // GET: Get ballot proof information

	// Census endpoints
	CensusURLParam                = "censusId"                                        // URL parameter for census ID
	NewCensusEndpoint             = "/censuses"                                       // POST: Create a new census
	AddCensusParticipantsEndpoint = "/censuses/{" + CensusURLParam + "}/participants" // POST: Add participants to census
	GetCensusParticipantsEndpoint = "/censuses/{" + CensusURLParam + "}/participants" // GET: Get census participants
	GetCensusRootEndpoint         = "/censuses/{" + CensusURLParam + "}/root"         // GET: Get census root
	GetCensusSizeEndpoint         = "/censuses/{" + CensusURLParam + "}/size"         // GET: Get census size
	DeleteCensusEndpoint          = "/censuses/{" + CensusURLParam + "}"              // DELETE: Delete census
	GetCensusProofEndpoint        = "/censuses/{" + CensusURLParam + "}/proof"        // GET: Get census proof

	// Worker endpoints
	WorkerUUIDParam         = "uuid"                                                                      // URL parameter for worker UUID
	WorkerAddressParam      = "address"                                                                   // URL parameter for worker address
	WorkersEndpoint         = "/workers"                                                                  // Base worker endpoint
	WorkerGetJobEndpoint    = WorkersEndpoint + "/{" + WorkerUUIDParam + "}/{" + WorkerAddressParam + "}" // GET: Worker get job
	WorkerSubmitJobEndpoint = WorkersEndpoint + "/{" + WorkerUUIDParam + "}"                              // POST: Worker submit job
	WorkersListEndpoint     = WorkersEndpoint                                                             // GET: List workers

	// Metadata endpoints
	MetadataHashParam   = "metadataHash"                                       // URL parameter for metadata hash
	MetadataSetEndpoint = "/metadata"                                          // POST: Set metadata
	MetadataGetEndpoint = MetadataSetEndpoint + "/{" + MetadataHashParam + "}" // GET: Get metadata
)

// EndpointWithParam creates an endpoint URL by replacing the parameter placeholder
// with the actual value. Used to build fully qualified endpoint URLs.
func EndpointWithParam(path, key, param string) string {
	return strings.Replace(path, "{"+key+"}", param, 1)
}

// LogExcludedPrefixes defines URL prefixes to exclude from request logging
var LogExcludedPrefixes = []string{
	PingEndpoint,
	WorkersEndpoint,
	InfoEndpoint,
}
