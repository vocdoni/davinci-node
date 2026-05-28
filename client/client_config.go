package client

import (
	"time"

	"github.com/vocdoni/davinci-node/web3"
)

const (
	// CreateProcessAction constant identifies the action of create a process
	// using a client
	CreateProcessAction = "create"
	// SubmitVoteAction constant identifies the action of submit votes using
	// a client
	SubmitVoteAction = "vote"
	// StopProcessAction constant identifies the action of stop a process and
	// get its results using a client
	StopProcessAction = "stop"
	// MinProcessDuration is the minimun duration of a process that can be
	// created with a client.
	MinProcessDuration = 10 * time.Minute
)

// ClientConfig struct includes the required configuration to create a client,
// including the Web3 configuration to interact with the Davinci contracts, and
// the sequencer and census3 service endpoints.
type ClientConfig struct {
	Web3              web3.Web3Config `mapstructure:"web3"`
	SequencerEndpoint string          `mapstructure:"sequencer"`
	Census3Endpoint   string          `mapstructure:"census3"`
}

// Valid method returns if the current configuration is valid or not. It only
// checks if the web3 config includes a non-empty private key, and also if the
// sequencer and census3 service endpoins are not empty
func (conf *ClientConfig) Valid() bool {
	return conf.Web3.PrivKey != "" && // web3
		conf.SequencerEndpoint != "" && conf.Census3Endpoint != "" // sequencz
}
