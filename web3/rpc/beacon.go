package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vocdoni/davinci-node/log"
)

const (
	// beaconConfigSpecPath is the Beacon API endpoint path to retrieve chain spec configuration.
	beaconConfigSpecPath = "/eth/v1/config/spec"
	// beaconConfigTimeout is the timeout for beacon API HTTP requests.
	beaconConfigTimeout = 10 * time.Second
)

// beaconSpecResponse represents the JSON response from /eth/v1/config/spec.
type beaconSpecResponse struct {
	Data *beaconSpecData `json:"data"`
}

// beaconSpecData holds the parsed fields from the spec endpoint.
type beaconSpecData struct {
	DepositNetworkID string `json:"DEPOSIT_NETWORK_ID"`
}

// BeaconChainID is the context-aware variant of BeaconChainID. It sends a
// GET request to the Beacon API /eth/v1/config/spec endpoint and extracts the
// chain ID from the DEPOSIT_NETWORK_ID field.
func BeaconChainID(ctx context.Context, beaconEndpoint string) (uint64, error) {
	if beaconEndpoint == "" {
		return 0, fmt.Errorf("beacon endpoint URL is empty")
	}

	// Normalise the endpoint URL: strip trailing slash before appending the path.
	base := strings.TrimRight(beaconEndpoint, "/")
	url := base + beaconConfigSpecPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create beacon spec request: %w", err)
	}

	client := &http.Client{Timeout: beaconConfigTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch beacon spec from %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Debugw("error closing beacon spec response body", "url", url, "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("beacon spec returned status %d from %s: %s", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}

	var specResp beaconSpecResponse
	if err := json.NewDecoder(resp.Body).Decode(&specResp); err != nil {
		return 0, fmt.Errorf("decode beacon spec response from %s: %w", url, err)
	}

	if specResp.Data == nil {
		return 0, fmt.Errorf("beacon spec response from %s contains no data", url)
	}

	if specResp.Data.DepositNetworkID == "" {
		return 0, fmt.Errorf("beacon spec response from %s has empty DEPOSIT_NETWORK_ID", url)
	}

	chainID, err := strconv.ParseUint(specResp.Data.DepositNetworkID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse DEPOSIT_NETWORK_ID %q from %s: %w",
			specResp.Data.DepositNetworkID, url, err)
	}

	log.Debugw("beacon chain ID resolved",
		"beaconEndpoint", beaconEndpoint,
		"chainID", chainID)

	return chainID, nil
}
