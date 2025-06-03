package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggingMiddleware(t *testing.T) {
	// Create a simple handler that echoes the body
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})

	// Wrap with logging middleware
	middleware := loggingMiddleware(100)
	wrappedHandler := middleware(handler)

	tests := []struct {
		name        string
		body        string
		shouldLog   bool
		description string
	}{
		{
			name:        "JSON object",
			body:        `{"key": "value"}`,
			shouldLog:   true,
			description: "Should log JSON objects",
		},
		{
			name:        "JSON array",
			body:        `[1, 2, 3]`,
			shouldLog:   true,
			description: "Should log JSON arrays",
		},
		{
			name:        "JSON with whitespace",
			body:        `  {"key": "value"}`,
			shouldLog:   true,
			description: "Should log JSON with leading whitespace",
		},
		{
			name:        "Binary data",
			body:        "\x00\x01\x02\x03\x04",
			shouldLog:   false,
			description: "Should not log binary data",
		},
		{
			name:        "Plain text",
			body:        "Hello, World!",
			shouldLog:   false,
			description: "Should not log plain text",
		},
		{
			name:        "Empty body",
			body:        "",
			shouldLog:   false,
			description: "Should handle empty body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			// Execute
			wrappedHandler.ServeHTTP(rec, req)

			// Check response
			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
			}

			// Check body was preserved
			respBody, _ := io.ReadAll(rec.Body)
			if string(respBody) != tt.body {
				t.Errorf("Body was modified: expected %q, got %q", tt.body, string(respBody))
			}
		})
	}
}

func TestLoggingMiddlewareExclusions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name        string
		path        string
		shouldSkip  bool
		description string
	}{
		{
			name:        "Ping endpoint",
			path:        "/ping",
			shouldSkip:  true,
			description: "Should skip ping endpoint",
		},
		{
			name:        "Workers endpoint",
			path:        "/workers/123/job",
			shouldSkip:  true,
			description: "Should skip workers endpoints",
		},
		{
			name:        "Regular endpoint",
			path:        "/votes",
			shouldSkip:  false,
			description: "Should not skip regular endpoints",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := loggingMiddleware(100)
			wrappedHandler := middleware(handler)

			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
			}
		})
	}
}

func TestLoggingConfigCustomExclusions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := LoggingConfig{
		MaxBodyLog: 100,
		ExcludedPrefixes: []string{
			"/api/v1/",
			"/health",
			"/metrics",
		},
	}

	middleware := loggingMiddlewareWithConfig(config)
	wrappedHandler := middleware(handler)

	tests := []struct {
		path       string
		shouldSkip bool
	}{
		{"/api/v1/users", true},
		{"/api/v1/posts/123", true},
		{"/health", true},
		{"/healthcheck", true}, // prefix match
		{"/metrics/prometheus", true},
		{"/api/v2/users", false},
		{"/users", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
			}
		})
	}
}

func TestResponseWriterCapture(t *testing.T) {
	// Test that responseWriter correctly captures status codes
	tests := []struct {
		name           string
		handlerFunc    func(w http.ResponseWriter, r *http.Request)
		expectedStatus int
	}{
		{
			name: "WriteHeader before Write",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("test"))
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "Write without WriteHeader",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("test"))
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "Multiple WriteHeader calls",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				w.WriteHeader(http.StatusAccepted) // Should be ignored
			},
			expectedStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rw := &responseWriter{
				ResponseWriter: rec,
				statusCode:     0,
			}

			req := httptest.NewRequest("GET", "/", nil)
			tt.handlerFunc(rw, req)

			if rw.statusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rw.statusCode)
			}
		})
	}
}

func BenchmarkLoggingMiddleware(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := loggingMiddleware(512)
	wrappedHandler := middleware(handler)

	jsonBody := `{"key": "value", "number": 123, "array": [1, 2, 3]}`

	b.Run("JSON body", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("POST", "/test", strings.NewReader(jsonBody))
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}
	})

	b.Run("Binary body", func(b *testing.B) {
		binaryBody := bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 100)
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("POST", "/test", bytes.NewReader(binaryBody))
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}
	})

	b.Run("No body", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}
	})
}
