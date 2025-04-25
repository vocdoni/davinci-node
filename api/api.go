package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	stg "github.com/vocdoni/vocdoni-z-sandbox/storage"
)

// APIConfig type represents the configuration for the API HTTP server.
// It includes the host, port and optionally an existing storage instance.
type APIConfig struct {
	Host    string
	Port    int
	Storage *stg.Storage // Optional: use existing storage instance
	Network string       // Optional: web3 network shortname
}

// API type represents the API HTTP server with JWT authentication capabilities.
type API struct {
	router  *chi.Mux
	storage *stg.Storage
	network string
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
		storage: conf.Storage,
		network: conf.Network,
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
	// votes endpoints
	log.Infow("register handler", "endpoint", VotesEndpoint, "method", "POST")
	a.router.Post(VotesEndpoint, a.newVote)
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
}

// bufPool is a pool of bytes.Buffer to reduce logger allocations.
var bufPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// initRouter creates the router with all the routes and middleware.
func (a *API) initRouter() {
	logHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Don't log if we're in debug or for PingEndpoint.
			if log.Level() != "debug" || r.URL.Path == PingEndpoint {
				next.ServeHTTP(w, r)
				return
			}

			// Get a buffer from the pool.
			buf := bufPool.Get().(*bytes.Buffer)
			buf.Reset()

			// Read the request body into the buffer.
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "unable to read request body", http.StatusInternalServerError)
				bufPool.Put(buf)
				return
			}

			// Write the bytes into our buffer.
			buf.Write(bodyBytes)

			// Log the request details.
			log.Debugw("api request",
				"method", r.Method,
				"url", r.URL.String(),
				"body", strings.ReplaceAll(buf.String(), "\"", ""),
			)

			// Restore the body for the next handler.
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			// Return the buffer to the pool.
			bufPool.Put(buf)

			next.ServeHTTP(w, r)
		})
	}

	a.router = chi.NewRouter()
	a.router.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}).Handler)
	a.router.Use(logHandler)
	a.router.Use(middleware.Recoverer)
	a.router.Use(middleware.Throttle(100))
	a.router.Use(middleware.ThrottleBacklog(5000, 40000, 60*time.Second))
	a.router.Use(middleware.Timeout(45 * time.Second))

	a.registerHandlers()
}
