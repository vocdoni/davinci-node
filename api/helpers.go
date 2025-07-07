package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/log"
)

// httpWriteJSON helper function allows to write a JSON response.
func httpWriteJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	jdata, err := json.Marshal(data)
	if err != nil {
		ErrMarshalingServerJSONFailed.WithErr(err).Write(w)
		return
	}
	n, err := w.Write(jdata)
	if err != nil {
		log.Warnw("failed to write http response", "error", err)
		return
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		log.Warnw("failed to write on response", "error", err)
		return
	}
	if !DisabledLogging && log.Level() == log.LogLevelDebug {
		log.Debugw("api response", "bytes", n, "data", strings.ReplaceAll(string(jdata), "\"", ""))
	}
}

// httpWriteBinary streams an in-memory byte slice as a response.
func httpWriteBinary(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if _, err := w.Write(data); err != nil {
		log.Warnw("failed to write binary response", "error", err)
		return
	}
}

// httpWriteOK helper function allows to write an OK response.
func httpWriteOK(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("\n")); err != nil {
		log.Warnw("failed to write on response", "error", err)
	}
}

// WorkerSeedToUUID converts a worker seed string into a UUID. It uses the
// first 16 bytes of the SHA256 hash of the seed to create a UUID.
func WorkerSeedToUUID(seed string) (*uuid.UUID, error) {
	var err error
	// Use the first 8 characters of the SHA256 hash of the seed as a UUID
	hash := sha256.Sum256([]byte(seed))
	u, err := uuid.FromBytes(hash[:16]) // Convert first 16 bytes to UUID
	if err != nil {
		return nil, fmt.Errorf("failed to create worker UUID: %w", err)
	}
	return &u, nil
}
