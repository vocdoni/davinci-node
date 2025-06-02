package api

import "strings"

const (
	// PingEndpoint is the endpoint for checking the API status
	PingEndpoint = "/ping"
	// ProcessesEndpoint is the endpoint for creating a new voting process
	ProcessesEndpoint = "/processes"
	// ProcessEndpoint is the endpoint to get the process info
	ProcessURLParam = "processId"
	ProcessEndpoint = "/processes/{" + ProcessURLParam + "}"
	// TestSetProcessEndpoint and TestProcessEndpoint is the endpoint for store
	// and retrieve the process info for testing. In a real scenatio, this
	// information should be in the smart contract.
	TestSetProcessEndpoint = "/processes/test"
	TestProcessEndpoint    = "/processes/test/{" + ProcessURLParam + "}"
	// VotesEndpoint is the endpoint for submitting a vote
	VotesEndpoint = "/votes"
	// VoteStatusEndpoint is the endpoint for checking the status of a vote
	VoteStatusProcessIDParam = "processId"
	VoteStatusVoteIDParam    = "voteId"
	VoteStatusEndpoint       = "/votes/status/{" + VoteStatusProcessIDParam + "}/{" + VoteStatusVoteIDParam + "}"
	// InfoEndpoint is the endpoint for getting ballot proof information
	InfoEndpoint = "/info"

	CensusURLParam = "censusID"
	// NewCensusEndpoint is the endpoint for creating a new census
	NewCensusEndpoint = "/censuses"
	// AddCensusParticipantsEndpoint is the endpoint for adding participants to a census
	AddCensusParticipantsEndpoint = "/censuses/{" + CensusURLParam + "}/participants"
	// GetCensusParticipantsEndpoint is the endpoint for getting the participants of a census
	GetCensusParticipantsEndpoint = "/censuses/{" + CensusURLParam + "}/participants"
	// GetCensusRootEndpoint is the endpoint for getting the root of a census
	GetCensusRootEndpoint = "/censuses/{" + CensusURLParam + "}/root"
	// GetCensusSizeEndpoint is the endpoint for getting the size of a census
	GetCensusSizeEndpoint = "/censuses/{" + CensusURLParam + "}/size"
	// DeleteCensusEndpoint is the endpoint for deleting a census
	DeleteCensusEndpoint = "/censuses/{" + CensusURLParam + "}"
	// GetCensusProofEndpoint is the endpoint for getting a proof of a census
	GetCensusProofEndpoint = "/censuses/{" + CensusURLParam + "}/proof"

	// Worker endpoints
	WorkerUUIDParam         = "uuid"
	WorkerAddressParam      = "address"
	WorkersEndpoint         = "/workers"
	WorkerGetJobEndpoint    = WorkersEndpoint + "/{" + WorkerUUIDParam + "}/{" + WorkerAddressParam + "}"
	WorkerSubmitJobEndpoint = WorkersEndpoint + "/{" + WorkerUUIDParam + "}"
	WorkersListEndpoint     = WorkersEndpoint
)

// EndpointWithParam replaces the key in the path with the param value
// provided. It is used to create the endpoint URL with the desired
// parameters.
func EndpointWithParam(path, key, param string) string {
	return strings.Replace(path, "{"+key+"}", param, 1)
}

// LogExcludedPrefixes returns the default list of URL prefixes to exclude from logging
var LogExcludedPrefixes = []string{
	PingEndpoint,
	WorkersEndpoint,
	InfoEndpoint,
}
