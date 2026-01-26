package census

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/vocdoni/davinci-node/census/censusdb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// JSONFormat represents the format of the JSON census dump.
type JSONFormat int

const (
	// JSONL represents the JSON Lines format (newline-delimited JSON). It is
	// used for large JSON dumps where each line is a separate JSON object and
	// it can be streamed.
	JSONL JSONFormat = iota
	// JSONArray represents the JSON Array format. It is used for smaller JSON
	// dumps where the entire dump is contained within a single JSON array.
	JSONArray
	// UnknownJSON represents an unknown JSON format.
	UnknownJSON
)

// String returns the string representation of the JSONFormat.
func (format JSONFormat) String() string {
	switch format {
	case JSONL:
		return "jsonl"
	case JSONArray:
		return "json"
	default:
		return "unknown"
	}
}

// JSONImporter method returns an instance of jsonImporter.
func JSONImporter() *jsonImporter {
	return new(jsonImporter)
}

// jsonImporter is an implementation of the ImporterPlugin interface for
// importing censuses from JSON dumps.
type jsonImporter struct{}

// ValidURI checks if the provided targetURI is a valid HTTP or HTTPS URL.
func (jsonImporter) ValidURI(targetURI string) bool {
	return strings.HasPrefix(targetURI, "http://") || strings.HasPrefix(targetURI, "https://")
}

// ImportCensus downloads the census merkle tree dump from the
// specified targetURL and imports it into the census DB based on the
// expectedRoot. It returns an error if the download or import fails.
func (jsonImporter) ImportCensus(
	ctx context.Context,
	censusDB *censusdb.CensusDB,
	census *types.Census,
	_ int,
) (int, error) {
	// Download the census merkle tree dump
	res, err := requestRawDump(ctx, census.CensusURI)
	if err != nil {
		return 0, fmt.Errorf("failed to download JSON dump from %s: %w", census.CensusURI, err)
	}
	// Ensure the response body is closed
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Warnw("failed to close JSON dump response body",
				"root", census.CensusRoot.String(),
				"uri", census.CensusURI,
				"error", err.Error())
		}
	}()
	// Create a reader that detects the JSON format
	jsonReader, jsonFormat, err := jsonReader(res)
	if err != nil {
		return 0, fmt.Errorf("failed to download census merkle tree from %s: %w", census.CensusURI, err)
	}
	size, err := importJSONDump(censusDB, jsonFormat, census.CensusRoot, jsonReader)
	if err != nil {
		return 0, fmt.Errorf("failed to import census merkle tree from %s: %w", census.CensusURI, err)
	}
	return size, nil
}

// requestRawDump performs an HTTP GET request to download the census raw dump
// from the specified URL. It returns the HTTP response or an error if the
// download fails.
func requestRawDump(ctx context.Context, targetURL string) (*http.Response, error) {
	// Create HTTP client with no timeout (can be adjusted as needed)
	client := &http.Client{
		Timeout: 0,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request for %s: %w", targetURL, err)
	}

	// Set both content type for JSONL and JSON array
	req.Header.Set("Accept", "application/x-ndjson, application/json;q=0.9, */*;q=0.1")

	// Perform the HTTP request
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download JSON dump from %s: %w", targetURL, err)
	}

	// Check for non-200 status codes
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("failed to download JSON dump from %s: status code %d, body: %s", targetURL, res.StatusCode, string(body))
	}
	return res, nil
}

// jsonReader determines the JSON format of the HTTP response by checking
// the Content-Type header and peeking into the content. It returns the
// detected JSONFormat and a reader for the response body. If the format
// cannot be determined, it returns UnknownJSON.
func jsonReader(res *http.Response) (io.Reader, JSONFormat, error) {
	// Check Content-Type header first
	contentType := strings.ToLower(res.Header.Get("Content-Type"))

	if strings.Contains(contentType, "ndjson") || strings.Contains(contentType, "jsonl") {
		return res.Body, JSONL, nil
	}
	if strings.Contains(contentType, "application/json") {
		return res.Body, JSONArray, nil
	}

	// Fallback: content based detection
	var buf bytes.Buffer
	tee := io.TeeReader(res.Body, &buf)

	// Create a json decoder to peek into the first non-whitespace character
	dec := json.NewDecoder(tee)
	var tmp any

	// Try to decode the first JSON token
	if err := dec.Decode(&tmp); err != nil {
		return io.MultiReader(&buf, res.Body), UnknownJSON, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	// Try to decode a second JSON token to determine if it's JSONL or JSONArray
	//  - If it works, multiple JSON objects were found, so it's JSONL
	//  - If it fails, it's a single JSON array
	if err := dec.Decode(&tmp); err == nil {
		return io.MultiReader(&buf, res.Body), JSONL, nil
	}
	return io.MultiReader(&buf, res.Body), JSONArray, nil
}

// importJSONDump imports the census merkle tree dump from the provided
// dataReader into the census DB based on the specified JSON format. It
// verifies that the imported census root matches the expected root.
func importJSONDump(
	censusDB *censusdb.CensusDB,
	format JSONFormat,
	expectedRoot types.HexBytes,
	dataReader io.Reader,
) (int, error) {
	// Import the census merkle tree dump into the census DB
	var err error
	var ref *censusdb.CensusRef
	switch format {
	case JSONL:
		// Import JSONL directly into census DB by expected root
		if ref, err = censusDB.Import(expectedRoot, dataReader); err != nil {
			return 0, fmt.Errorf("failed to import %s census dump, with expected root '%s': %w", format.String(), expectedRoot.String(), err)
		}
	case JSONArray:
		// Read entire JSON array dump
		dump, err := io.ReadAll(dataReader)
		if err != nil {
			return 0, fmt.Errorf("failed to read %s census dump, with expected root '%s': %w", format.String(), expectedRoot.String(), err)
		}
		// Import JSON array dump into census DB

		if ref, err = censusDB.ImportAll(dump); err != nil {
			return 0, fmt.Errorf("failed to import %s census dump, with expected root '%s': %w", format.String(), expectedRoot.String(), err)
		}
		// Verify the imported census root matches the expected root
		if !bytes.Equal(ref.Root(), expectedRoot) {
			return 0, fmt.Errorf("imported census root mismatch: expected %s, got %x", expectedRoot.String(), ref.Root())
		}
	default:
		return 0, fmt.Errorf("unknown JSON format: %s", format.String())
	}
	return ref.Size(), nil
}
