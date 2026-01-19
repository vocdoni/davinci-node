package helpers

import (
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/util"
)

const (
	// first account private key created by anvil with default mnemonic
	LocalAccountPrivKey   = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	LocalCSPSeed          = "1f1e0cd27b4ecd1b71b6333790864ace2870222c"
	WorkerSeed            = "test-seed"
	WorkerTokenExpiration = 24 * time.Hour
	WorkerTimeout         = time.Second * 5
	// envarionment variable names
	DeployerServerPortEnvVarName      = "DEPLOYER_SERVER"                        // environment variable name for deployer server port
	ContractsBranchNameEnvVarName     = "SEQUENCER_CONTRACTS_BRANCH"             // environment variable name for z-contracts branch
	ContractsCommitHashEnvVarName     = "SEQUENCER_CONTRACTS_COMMIT"             // environment variable name for z-contracts commit hash
	PrivKeyEnvVarName                 = "SEQUENCER_PRIV_KEY"                     // environment variable name for private key
	RPCUrlEnvVarName                  = "SEQUENCER_RPC_URL"                      // environment variable name for RPC URL
	AnvilPortEnvVarName               = "ANVIL_PORT_RPC_HTTP"                    // environment variable name for Anvil port
	OrgRegistryEnvVarName             = "SEQUENCER_ORGANIZATION_REGISTRY"        // environment variable name for organization registry
	ProcessRegistryEnvVarName         = "SEQUENCER_PROCESS_REGISTRY"             // environment variable name for process registry
	ResultsVerifierEnvVarName         = "SEQUENCER_RESULTS_ZK_VERIFIER"          // environment variable name for results zk verifier
	StateTransitionVerifierEnvVarName = "SEQUENCER_STATE_TRANSITION_ZK_VERIFIER" // environment variable name for state transition zk verifier
	CSPCensusEnvVarName               = "CSP_CENSUS"                             // environment variable name to select between csp or merkle tree census (by default merkle tree)

	DefaultBatchTimeWindow = 45 * time.Second // default batch time window for sequencer
)

var (
	DefaultAPIPort     = util.RandomInt(40000, 60000)
	DefaultCensus3Port = util.RandomInt(40000, 60000)
	DefaultCensus3URL  = fmt.Sprintf("http://localhost:%d", DefaultCensus3Port)
)
