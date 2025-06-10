package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/log"
	stg "github.com/vocdoni/davinci-node/storage"
)

const (
	maxRequestBodyLog = 512 // Maximum length of request body to log
)

// APIConfig type represents the configuration for the API HTTP server.
// It includes the host, port and optionally an existing storage instance.
type APIConfig struct {
	Host    string
	Port    int
	Storage *stg.Storage // Optional: use existing storage instance
	Network string       // Optional: web3 network shortname
	// Worker configuration
	WorkerEnabled bool          // Enable worker API endpoints
	WorkerUrlSeed string        // URL seed for worker authentication
	WorkerTimeout time.Duration // Worker job timeout
}

// API type represents the API HTTP server with JWT authentication capabilities.
type API struct {
	router  *chi.Mux
	storage *stg.Storage
	network string
	// Worker fields
	workerUUID    *uuid.UUID
	workerTimeout time.Duration
	// Worker job tracking: voteID -> workerJob
	activeJobs map[string]*workerJob
	jobsMutex  sync.RWMutex
}

type workerJob struct {
	VoteID    []byte
	Address   string
	Timestamp time.Time
}

// New creates a new API instance with the given configuration.
// It also initializes the storage and starts the HTTP server.
func New(conf *APIConfig) (*API, error) {
	if conf == nil {
		return nil, fmt.Errorf("missing API configuration")
	}
	if conf.Storage == nil {
		return nil, fmt.Errorf("missing storage instance")
	}
	a := &API{
		storage:       conf.Storage,
		network:       conf.Network,
		workerTimeout: conf.WorkerTimeout,
		activeJobs:    make(map[string]*workerJob),
	}

	// Initialize worker UUID if enabled
	if conf.WorkerUrlSeed != "" {
		var err error
		hash := sha256.Sum256([]byte(conf.WorkerUrlSeed))
		u, err := uuid.FromBytes(hash[:16]) // Convert first 16 bytes to UUID
		if err != nil {
			return nil, fmt.Errorf("failed to create worker UUID: %w", err)
		}
		a.workerUUID = &u
		log.Infow("worker API enabled", "url", fmt.Sprintf("%s/%s", WorkersEndpoint, a.workerUUID))

		// Start timeout monitor
		a.startWorkerTimeoutMonitor()
	}

	// Initialize router
	a.initRouter()
	go func() {
		log.Infow("Starting API server", "host", conf.Host, "port", conf.Port)
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
	// The following endpoints are registered:
	// - GET /ping: No parameters
	// - POST /process: No parameters
	// - GET /process: No parameters
	// - POST /census: No parameters
	// - POST /census/<uuid>/participants: No parameters
	// - GET /census/<uuid>/participants: No parameters
	// - GET /census/<uuid>/root: No parameters
	// - GET /census/<uuid or root>/size: No parameters
	// - DELETE /census/<uuid>: No parameters
	// - GET /census/<root>/proof?key=<key>: Parameters: key
	log.Infow("register handler", "endpoint", PingEndpoint, "method", "GET")
	a.router.Get(PingEndpoint, func(w http.ResponseWriter, r *http.Request) {
		httpWriteOK(w)
	})
	// processes endpoints
	log.Infow("register handler", "endpoint", ProcessesEndpoint, "method", "POST")
	a.router.Post(ProcessesEndpoint, a.newProcess)
	log.Infow("register handler", "endpoint", ProcessEndpoint, "method", "GET")
	a.router.Get(ProcessEndpoint, a.process)
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
	log.Infow("register handler", "endpoint", InfoEndpoint, "method", "GET")
	a.router.Get(InfoEndpoint, a.info)
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

	// worker endpoints (if enabled)
	if a.workerUUID != nil {
		log.Infow("register handler", "endpoint", WorkerGetJobEndpoint, "method", "GET")
		a.router.Get(WorkerGetJobEndpoint, a.workerGetJob)
		log.Infow("register handler", "endpoint", WorkerSubmitJobEndpoint, "method", "POST")
		a.router.Post(WorkerSubmitJobEndpoint, a.workerSubmitJob)
		log.Infow("register handler", "endpoint", WorkersListEndpoint, "method", "GET")
		a.router.Get(WorkersListEndpoint, a.workersList)
	}
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

	a.registerHandlers()
}
