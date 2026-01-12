package api

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// DisabledLogging is a global flag to disable logging middleware
var DisabledLogging = false

// jsonRegex matches common JSON starting patterns
var jsonRegex = regexp.MustCompile(`^\s*[\[{]`)

// LoggingConfig holds configuration for the logging middleware
type LoggingConfig struct {
	MaxBodyLog       int
	ExcludedPrefixes []string // URL path prefixes to exclude from logging
}

// DefaultLoggingConfig returns a LoggingConfig with sensible defaults
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		MaxBodyLog:       512,
		ExcludedPrefixes: LogExcludedPrefixes,
	}
}

// shouldSkipLogging checks if the request should be skipped from logging
func (lc LoggingConfig) shouldSkipLogging(r *http.Request) bool {
	// Skip if not in debug mode
	if log.Level() != log.LogLevelDebug {
		return true
	}
	// Skip if logging is disabled
	if DisabledLogging {
		return true
	}
	// Check if path matches any excluded prefix
	path := r.URL.Path
	for _, prefix := range lc.ExcludedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.statusCode == 0 {
		rw.statusCode = code
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}

// loggingMiddleware provides request/response logging for debugging
func loggingMiddleware(maxBodyLog int) func(http.Handler) http.Handler {
	config := LoggingConfig{
		MaxBodyLog:       maxBodyLog,
		ExcludedPrefixes: DefaultLoggingConfig().ExcludedPrefixes,
	}
	return loggingMiddlewareWithConfig(config)
}

// loggingMiddlewareWithConfig provides request/response logging with custom configuration
func loggingMiddlewareWithConfig(config LoggingConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging on specific endpoints if configured (e.g., for health checks or other endpoints)
			if config.shouldSkipLogging(r) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()

			// Prepare to read body if needed
			var bodyStr string

			if r.Body != nil && r.ContentLength > 0 {
				// Read body into buffer
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					log.Error(err)
					http.Error(w, "unable to read request body", http.StatusInternalServerError)
					return
				}

				// Restore body for handler
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

				// Check if it looks like JSON (starts with { or [ after optional whitespace)
				if jsonRegex.Match(bodyBytes) {
					bodyStr = string(bodyBytes)
					if len(bodyStr) > config.MaxBodyLog {
						bodyStr = bodyStr[:config.MaxBodyLog] + "..."
					}
					// Remove quotes for cleaner logs
					bodyStr = strings.ReplaceAll(bodyStr, "\"", "")
				}
			}

			// Wrap response writer
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     0,
			}

			// Log request
			log.Debugw("api request",
				"method", r.Method,
				"url", r.URL.String(),
				"body", bodyStr,
			)

			// Process request
			next.ServeHTTP(wrapped, r)

			// Log response
			duration := time.Since(start)
			log.Debugw("api response",
				"method", r.Method,
				"url", r.URL.String(),
				"status", wrapped.statusCode,
				"took", duration.String(),
			)
		})
	}
}

// skipUnknownProcessIDMiddleware allows to skip requests with unknown
// ProcessID versions. It checks the "processID" URL parameter and compares
// its version against the provided currentVersion. If they don't match, it
// responds with 404 Not Found. If the path does not contain a processID
// parameter, it simply calls the next handler.
func skipUnknownProcessIDMiddleware(currentVersion [4]byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if the route contains a process id
			processIDStr := chi.URLParam(r, ProcessURLParam)
			if processIDStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			processID, err := types.HexStringToProcessID(processIDStr)
			if err != nil {
				ErrMalformedProcessID.Withf("could not parse process ID: %v", err).Write(w)
				return
			}

			// Check if the process id is valid
			if !processID.IsValid() {
				ErrMalformedProcessID.Withf("invalid process ID: %s", processIDStr).Write(w)
				return
			}
			// Check if the version matches the current expected version
			if processID.Version() != currentVersion {
				http.NotFound(w, r)
				return
			}
			// Continue to the next handler
			next.ServeHTTP(w, r)
		})
	}
}
