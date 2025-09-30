package api

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	stg "github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/workers"
)

const (
	maxRequestBodyLog = 512      // Maximum length of request body to log
	webappdir         = "webapp" // Directory where the web application files are located
)

// APIConfig type represents the configuration for the API HTTP server.
// It includes the host, port and optionally an existing storage instance.
type APIConfig struct {
	Host       string
	Port       int
	Storage    *stg.Storage // Optional: use existing storage instance
	Network    string       // Optional: web3 network shortname
	Web3Config config.DavinciWeb3Config
	// Worker configuration
	SequencerWorkersSeed       string                  // Seed for workers authentication over current sequencer
	WorkersAuthtokenExpiration time.Duration           // Expiration time for worker authentication tokens
	WorkerJobTimeout           time.Duration           // Worker job timeout
	WorkerBanRules             *workers.WorkerBanRules // Custom ban rules for workers
}

// API type represents the API HTTP server with JWT authentication capabilities.
type API struct {
	router            *chi.Mux
	storage           *stg.Storage
	network           string
	web3Config        config.DavinciWeb3Config
	processIDsVersion []byte // Current process ID version
	// Workers API stuff
	sequencerSigner            *ethereum.Signer        // Signer for workers authentication
	sequencerUUID              *uuid.UUID              // UUID to keep the workers endpoints hidden
	workersAuthtokenExpiration time.Duration           // Expiration time for worker authentication tokens
	workersJobTimeout          time.Duration           // The time that the sequencer waits for a worker job
	workersBanRules            *workers.WorkerBanRules // Rules for banning workers based on job failures
	jobsManager                *workers.JobsManager    // Manages worker jobs and timeouts
	parentCtx                  context.Context         // Context to stop the API server
}

// New creates a new API instance with the given configuration.
// It also initializes the storage and starts the HTTP server.
func New(ctx context.Context, conf *APIConfig) (*API, error) {
	if conf == nil {
		return nil, fmt.Errorf("missing API configuration")
	}
	if conf.Storage == nil {
		return nil, fmt.Errorf("missing storage instance")
	}

	// Initialize the API
	a := &API{
		storage:                    conf.Storage,
		network:                    conf.Network,
		web3Config:                 conf.Web3Config,
		workersJobTimeout:          conf.WorkerJobTimeout,
		workersAuthtokenExpiration: conf.WorkersAuthtokenExpiration,
		parentCtx:                  ctx,
	}

	// Set the supported process ID versions
	currentProcessIDVersion, err := a.ProcessIDVersion()
	if err != nil {
		return nil, fmt.Errorf("could not determine current process ID version: %w", err)
	}
	a.processIDsVersion = currentProcessIDVersion

	// If no ban rules for workers are provided, use default rules
	if conf.WorkerBanRules != nil {
		a.workersBanRules = conf.WorkerBanRules
	}

	// Initialize router
	a.initRouter()

	// Try to start workers API
	if err := a.startWorkersAPI(*conf); err != nil {
		return nil, fmt.Errorf("failed to start workers API: %w", err)
	}

	// Initialize the server in background
	go func() {
		log.Infow("starting API server", "host", conf.Host, "port", conf.Port)
		if err := http.ListenAndServe(fmt.Sprintf("%s:%d", conf.Host, conf.Port), a.router); err != nil {
			log.Fatalf("failed to start the API server: %v", err)
		}
	}()
	return a, nil
}

// Router returns the chi router for testing purposes
func (a *API) Router() *chi.Mux {
	return a.router
}

// registerHandlers registers all the HTTP handlers for the API endpoints.
func (a *API) registerHandlers() {
	// health check endpoint
	log.Infow("register handler", "endpoint", PingEndpoint, "method", "GET")
	a.router.Get(PingEndpoint, func(w http.ResponseWriter, r *http.Request) {
		httpWriteOK(w)
	})

	// info endpoint
	log.Infow("register handler", "endpoint", InfoEndpoint, "method", "GET")
	a.router.Get(InfoEndpoint, a.info)

	// host load endpoint
	log.Infow("register handler", "endpoint", HostLoadEndpoint, "method", "GET")
	a.router.Get(HostLoadEndpoint, a.hostLoad)

	// static file serving
	log.Infow("register static handler", "endpoint", "/app/*", "method", "GET")
	a.router.Get(StaticFilesEndpoint, staticHandler)

	// processes endpoints
	log.Infow("register handler", "endpoint", ProcessesEndpoint, "method", "POST")
	a.router.Post(ProcessesEndpoint, a.newProcess)
	log.Infow("register handler", "endpoint", ProcessEndpoint, "method", "GET")
	a.router.Get(ProcessEndpoint, a.process)
	log.Infow("register handler", "endpoint", ProcessesEndpoint, "method", "GET")
	a.router.Get(ProcessesEndpoint, a.processList)

	// metadata endpoints
	log.Infow("register handler", "endpoint", MetadataSetEndpoint, "method", "POST")
	a.router.Post(MetadataSetEndpoint, a.setMetadata)
	log.Infow("register handler", "endpoint", MetadataGetEndpoint, "method", "GET")
	a.router.Get(MetadataGetEndpoint, a.fetchMetadata)

	// votes endpoints
	log.Infow("register handler", "endpoint", VotesEndpoint, "method", "POST")
	a.router.Post(VotesEndpoint, a.newVote)
	log.Infow("register handler", "endpoint", VoteStatusEndpoint, "method", "GET")
	a.router.Get(VoteStatusEndpoint, a.voteStatus)
	log.Infow("register handler", "endpoint", VoteByAddressEndpoint, "method", "GET")
	a.router.Get(VoteByAddressEndpoint, a.voteByAddress)

	// census endpoints
	log.Infow("register handler", "endpoint", NewCensusEndpoint, "method", "POST")
	a.router.Post(NewCensusEndpoint, a.newCensus)
	log.Infow("register handler", "endpoint", AddCensusParticipantsEndpoint, "method", "POST")
	a.router.Post(AddCensusParticipantsEndpoint, a.addCensusParticipants)
	log.Infow("register handler", "endpoint", GetCensusParticipantsEndpoint, "method", "GET")
	a.router.Get(GetCensusParticipantsEndpoint, a.getCensusParticipants)
	log.Infow("register handler", "endpoint", GetCensusRootEndpoint, "method", "GET")
	a.router.Get(GetCensusRootEndpoint, a.getCensusRoot)
	log.Infow("register handler", "endpoint", GetCensusSizeEndpoint, "method", "GET")
	a.router.Get(GetCensusSizeEndpoint, a.getCensusSize)
	log.Infow("register handler", "endpoint", DeleteCensusEndpoint, "method", "DELETE")
	a.router.Delete(DeleteCensusEndpoint, a.deleteCensus)
	log.Infow("register handler", "endpoint", GetCensusProofEndpoint, "method", "GET", "parameters", "key")
	a.router.Get(GetCensusProofEndpoint, a.getCensusProof)

	// sequencer workers stats endpoint - available even without worker mode
	log.Infow("register handler", "endpoint", SequencerWorkersEndpoint, "method", "GET")
	a.router.Get(SequencerWorkersEndpoint, a.workersList)
}

// initRouter creates the router with all the routes and middleware.
func (a *API) initRouter() {
	a.router = chi.NewRouter()
	a.router.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}).Handler)
	a.router.Use(loggingMiddleware(maxRequestBodyLog))
	a.router.Use(middleware.Recoverer)
	a.router.Use(middleware.Throttle(100))
	a.router.Use(middleware.ThrottleBacklog(5000, 40000, 60*time.Second))
	a.router.Use(middleware.Timeout(45 * time.Second))
	// Add middleware to skip unknown process ID versions
	a.router.Use(skipUnknownProcessIDMiddleware(a.processIDsVersion))

	a.registerHandlers()
}

// staticHandler serves static files from the webapp directory.
func staticHandler(w http.ResponseWriter, r *http.Request) {
	var filePath string
	if r.URL.Path == "/app" || r.URL.Path == "/app/" {
		filePath = path.Join(webappdir, "index.html")
	} else {
		filePath = path.Join(webappdir, strings.TrimPrefix(path.Clean(r.URL.Path), "/app"))
	}
	// Serve the file using http.ServeFile
	http.ServeFile(w, r, filePath)
}

// ProcessIDVersion returns the expected ProcessID version for the current
// network and contract address. It can be used to validate ProcessIDs.
func (a *API) ProcessIDVersion() ([]byte, error) {
	chainID, ok := npbindings.AvailableNetworksByName[a.network]
	if !ok {
		return nil, fmt.Errorf("unknown network: %s", a.network)
	}
	contractAddr := common.HexToAddress(a.web3Config.ProcessRegistrySmartContract)
	return types.ProcessIDVersion(chainID, contractAddr), nil
}
