package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/metadata"
	stg "github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/workers"
)

const (
	maxRequestBodyLog = 512      // Maximum length of request body to log
	webappdir         = "webapp" // Directory where the web application files are located
	appRouteRoot      = "/app"
)

var webappFileServer = http.StripPrefix(appRouteRoot+"/", http.FileServer(http.FS(os.DirFS(webappdir))))

// APIConfig type represents the configuration for the API HTTP server.
// It includes the host, port and optionally an existing storage instance.
type APIConfig struct {
	Host     string
	Port     int
	Storage  *stg.Storage // Optional: use existing storage instance
	Runtimes *web3.RuntimeRouter
	// Worker configuration
	SequencerWorkersSeed       string                  // Seed for workers authentication over current sequencer
	WorkersAuthtokenExpiration time.Duration           // Expiration time for worker authentication tokens
	WorkerJobTimeout           time.Duration           // Worker job timeout
	WorkerBanRules             *workers.WorkerBanRules // Custom ban rules for workers
	// Metadata configuration
	PinataConfig metadata.PinataMetadataProviderConfig // Pinata configuration
}

// API type represents the API HTTP server with JWT authentication capabilities.
type API struct {
	router       *chi.Mux
	storage      *stg.Storage
	metadata     *metadata.MetadataStorage
	runtimes     *web3.RuntimeRouter
	networksInfo map[uint64]SequencerNetworkInfo
	// Workers API stuff
	sequencerSigner            *ethereum.Signer         // Signer for workers authentication
	sequencerUUID              *uuid.UUID               // UUID to keep the workers endpoints hidden
	voteVerifier               *circuits.CircuitRuntime // VoteVerifier circuit
	workersAuthtokenExpiration time.Duration            // Expiration time for worker authentication tokens
	workersJobTimeout          time.Duration            // The time that the sequencer waits for a worker job
	workersBanRules            *workers.WorkerBanRules  // Rules for banning workers based on job failures
	jobsManager                *workers.JobsManager     // Manages worker jobs and timeouts
	parentCtx                  context.Context          // Context to stop the API server
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
	if conf.Runtimes == nil {
		return nil, fmt.Errorf("missing runtime router")
	}

	runtimeInfos, err := apiRuntimeData(conf.Runtimes)
	if err != nil {
		return nil, fmt.Errorf("invalid runtime router: %w", err)
	}

	// By default, use the local metadata provider
	metadataProviders := []metadata.MetadataProvider{
		metadata.NewLocalMetadata(conf.Storage.DB()),
	}
	// If Pinata configuration is provided, add the Pinata provider
	if conf.PinataConfig.Valid() {
		log.Debugw("valid pinata config provided", "gatewayURL", conf.PinataConfig.GatewayURL, "hostnameURL", conf.PinataConfig.HostnameURL)
		metadataProviders = append(metadataProviders, metadata.NewPinataMetadataProvider(conf.PinataConfig))
	}

	// Initialize the API
	a := &API{
		storage:                    conf.Storage,
		metadata:                   metadata.New(metadata.CID, metadataProviders...),
		runtimes:                   conf.Runtimes,
		networksInfo:               runtimeInfos,
		workersJobTimeout:          conf.WorkerJobTimeout,
		workersAuthtokenExpiration: conf.WorkersAuthtokenExpiration,
		parentCtx:                  ctx,
	}

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
	log.Infow("register handler", "endpoint", ProcessEndpoint, "method", "GET")
	a.router.Get(ProcessEndpoint, a.process)
	log.Infow("register handler", "endpoint", ProcessesEndpoint, "method", "GET")
	a.router.Get(ProcessesEndpoint, a.processList)
	log.Infow("register handler", "endpoint", CensusParticipantEndpoint, "method", "GET")
	a.router.Get(CensusParticipantEndpoint, a.processParticipant)
	log.Infow("register handler", "endpoint", NewEncryptionKeysEndpoint, "method", "POST")
	a.router.Post(NewEncryptionKeysEndpoint, a.processEncryptionKeys)

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
	log.Infow("register handler", "endpoint", BallotByIndexEndpoint, "method", "GET")
	a.router.Get(BallotByIndexEndpoint, a.ballotByIndex)

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
		AllowCredentials: false,
		MaxAge:           300,
	}).Handler)
	a.router.Use(loggingMiddleware(maxRequestBodyLog))
	a.router.Use(middleware.Recoverer)
	a.router.Use(middleware.Throttle(100))
	a.router.Use(middleware.ThrottleBacklog(5000, 40000, 60*time.Second))
	a.router.Use(middleware.Timeout(45 * time.Second))
	// Add middleware to skip unknown process ID versions
	a.router.Use(skipUnknownProcessIDMiddleware(a.runtimes))

	a.registerHandlers()
}

// staticHandler serves static files from the webapp directory.
func staticHandler(w http.ResponseWriter, r *http.Request) {
	appRoutePrefix := appRouteRoot + "/"

	if r.URL.Path == appRouteRoot || r.URL.Path == appRoutePrefix {
		http.ServeFile(w, r, path.Join(webappdir, "index.html"))
		return
	}
	if !strings.HasPrefix(r.URL.Path, appRoutePrefix) {
		http.NotFound(w, r)
		return
	}
	requestPath := strings.TrimPrefix(r.URL.Path, appRoutePrefix)
	staticPath := path.Clean(requestPath)
	if staticPath == "." {
		if r.URL.Path != appRoutePrefix {
			redirectURL := *r.URL
			redirectURL.Path = appRoutePrefix
			redirectURL.RawPath = ""
			http.Redirect(w, r, redirectURL.String(), http.StatusMovedPermanently)
			return
		}
		http.ServeFile(w, r, path.Join(webappdir, "index.html"))
		return
	}
	if staticPath == ".." || strings.HasPrefix(staticPath, "../") {
		http.NotFound(w, r)
		return
	}
	canonicalPath := appRoutePrefix + staticPath
	if strings.HasSuffix(requestPath, "/") {
		canonicalPath += "/"
	}
	if r.URL.Path != canonicalPath {
		redirectURL := *r.URL
		redirectURL.Path = canonicalPath
		redirectURL.RawPath = ""
		http.Redirect(w, r, redirectURL.String(), http.StatusMovedPermanently)
		return
	}

	webappFileServer.ServeHTTP(w, r)
}

func apiRuntimeData(router *web3.RuntimeRouter) (map[uint64]SequencerNetworkInfo, error) {
	runtimes := router.Runtimes()
	if len(runtimes) == 0 {
		return nil, fmt.Errorf("no runtimes configured")
	}

	runtimeInfos := make(map[uint64]SequencerNetworkInfo, len(runtimes))
	for _, runtime := range runtimes {
		if runtime == nil {
			return nil, fmt.Errorf("nil runtime")
		}
		if runtime.Contracts == nil {
			return nil, fmt.Errorf("runtime for chainID %d has nil contracts", runtime.ChainID)
		}
		if runtime.Contracts.ContractsAddresses == nil {
			return nil, fmt.Errorf("runtime for chainID  %d has nil contract addresses", runtime.ChainID)
		}

		if _, exists := runtimeInfos[runtime.ChainID]; exists {
			return nil, fmt.Errorf("duplicate runtime chain ID %d", runtime.ChainID)
		}

		contractsInfo, err := apiContractAddresses(runtime.Contracts)
		if err != nil {
			return nil, fmt.Errorf("runtime for chainID  %d: %w", runtime.ChainID, err)
		}

		runtimeInfos[runtime.ChainID] = SequencerNetworkInfo{
			ChainID:                 runtime.ChainID,
			ShortName:               runtime.ShortName,
			ProcessRegistryContract: contractsInfo.ProcessRegistry,
			ProcessIDVersion:        runtime.ProcessIDVersion[:],
		}
	}
	return runtimeInfos, nil
}

func apiContractAddresses(contracts *web3.Contracts) (ContractAddresses, error) {
	if contracts == nil {
		return ContractAddresses{}, fmt.Errorf("nil contracts")
	}
	if contracts.ContractsAddresses == nil {
		return ContractAddresses{}, fmt.Errorf("nil contract addresses")
	}

	processRegistry := contracts.ContractsAddresses.ProcessRegistry
	if processRegistry == (common.Address{}) {
		return ContractAddresses{}, fmt.Errorf("process registry address is required")
	}

	stateTransition := contracts.ContractsAddresses.StateTransitionZKVerifier
	if stateTransition == (common.Address{}) {
		addr, err := contracts.StateTransitionVerifierAddress()
		if err != nil {
			return ContractAddresses{}, fmt.Errorf("resolve state transition verifier address: %w", err)
		}
		stateTransition = common.HexToAddress(addr)
	}
	if stateTransition == (common.Address{}) {
		return ContractAddresses{}, fmt.Errorf("state transition verifier address is required")
	}

	results := contracts.ContractsAddresses.ResultsZKVerifier
	if results == (common.Address{}) {
		addr, err := contracts.ResultsVerifierAddress()
		if err != nil {
			return ContractAddresses{}, fmt.Errorf("resolve results verifier address: %w", err)
		}
		results = common.HexToAddress(addr)
	}
	if results == (common.Address{}) {
		return ContractAddresses{}, fmt.Errorf("results verifier address is required")
	}

	return ContractAddresses{
		ProcessRegistry:           processRegistry.String(),
		StateTransitionZKVerifier: stateTransition.String(),
		ResultsZKVerifier:         results.String(),
	}, nil
}
