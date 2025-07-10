package api

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/log"
	stg "github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/workers"
)

const (
	maxRequestBodyLog = 512      // Maximum length of request body to log
	webappdir         = "webapp" // Directory where the web application files are located
)

// APIConfig type represents the configuration for the API HTTP server.
// It includes the host, port and optionally an existing storage instance.
type APIConfig struct {
	Host    string
	Port    int
	Storage *stg.Storage // Optional: use existing storage instance
	Network string       // Optional: web3 network shortname
	// Worker configuration
	WorkerEnabled bool                    // Enable worker API endpoints
	WorkerUrlSeed string                  // URL seed for worker authentication
	WorkerTimeout time.Duration           // Worker job timeout
	BanRules      *workers.WorkerBanRules // Custom ban rules for workers
}

// API type represents the API HTTP server with JWT authentication capabilities.
type API struct {
	router  *chi.Mux
	storage *stg.Storage
	network string
	// Worker fields
	workerUUID    *uuid.UUID
	workerTimeout time.Duration
	jobsManager   *workers.JobsManager    // Manages worker jobs and timeouts
	parentCtx     context.Context         // Context to stop the API server
	banRules      *workers.WorkerBanRules // Rules for banning workers based on job failures
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

	a := &API{
		storage:       conf.Storage,
		network:       conf.Network,
		workerTimeout: conf.WorkerTimeout,
		parentCtx:     ctx,
	}
	if conf.BanRules != nil {
		a.banRules = conf.BanRules
	}

	// Initialize worker UUID if enabled
	if conf.WorkerUrlSeed != "" {
		var err error
		a.workerUUID, err = workers.WorkerSeedToUUID(conf.WorkerUrlSeed)
		if err != nil {
			return nil, fmt.Errorf("failed to create worker UUID: %w", err)
		}
		log.Infow("worker API enabled", "url", fmt.Sprintf("%s/%s", WorkersEndpoint, a.workerUUID))

		// Start timeout monitor
		a.startWorkerTimeoutMonitor()
	}

	// Initialize router
	a.initRouter()
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

	// worker endpoints (if enabled)
	if a.workerUUID != nil {
		log.Infow("register handler", "endpoint", WorkerGetJobEndpoint, "method", "GET")
		a.router.Get(WorkerGetJobEndpoint, a.workerGetJob)
		log.Infow("register handler", "endpoint", WorkerSubmitJobEndpoint, "method", "POST")
		a.router.Post(WorkerSubmitJobEndpoint, a.workerSubmitJob)
	}

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
