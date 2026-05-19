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

	"github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/vocdoni/davinci-node/log"
)

const (
	// beaconConfigSpecPath is the Beacon API endpoint path to retrieve chain spec configuration.
	beaconConfigSpecPath = "/eth/v1/config/spec"
	// beaconGenesisPath is the Beacon API endpoint path to retrieve genesis information.
	beaconGenesisPath = "/eth/v1/beacon/genesis"
	// beaconBlobSidecarsPath is the Beacon API endpoint path to retrieve blob sidecars by slot.
	beaconBlobSidecarsPath = "/eth/v1/beacon/blob_sidecars/%d"
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

type beaconGenesisResponse struct {
	Data *v1.Genesis `json:"data"`
}

type beaconBlobSidecarsResponse struct {
	Data []*deneb.BlobSidecar `json:"data"`
}

func fetchBeaconEndpoint(ctx context.Context, beaconEndpoint, endpointPath string) ([]byte, string, error) {
	if beaconEndpoint == "" {
		return nil, "", fmt.Errorf("beacon endpoint URL is empty")
	}

	base := strings.TrimRight(beaconEndpoint, "/")
	url := base + endpointPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create beacon request: %w", err)
	}

	client := &http.Client{Timeout: beaconConfigTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch beacon endpoint %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Debugw("error closing beacon response body", "url", url, "error", cerr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read beacon response from %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("beacon endpoint returned status %d from %s: %s", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}

	return body, url, nil
}

// BeaconChainID is the context-aware variant of BeaconChainID. It sends a
// GET request to the Beacon API /eth/v1/config/spec endpoint and extracts the
// chain ID from the DEPOSIT_NETWORK_ID field.
func BeaconChainID(ctx context.Context, beaconEndpoint string) (uint64, error) {
	body, url, err := fetchBeaconEndpoint(ctx, beaconEndpoint, beaconConfigSpecPath)
	if err != nil {
		return 0, err
	}
	var specResp beaconSpecResponse
	if err := json.Unmarshal(body, &specResp); err != nil {
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

// BeaconTiming returns the beacon genesis time and slot duration for the chain.
func BeaconTiming(ctx context.Context, beaconEndpoint string) (uint64, time.Duration, error) {
	genesisBody, genesisURL, err := fetchBeaconEndpoint(ctx, beaconEndpoint, beaconGenesisPath)
	if err != nil {
		return 0, 0, err
	}
	var genesisResp beaconGenesisResponse
	if err := json.Unmarshal(genesisBody, &genesisResp); err != nil {
		return 0, 0, fmt.Errorf("decode beacon genesis response from %s: %w", genesisURL, err)
	}
	if genesisResp.Data == nil {
		return 0, 0, fmt.Errorf("beacon genesis response from %s contains no data", genesisURL)
	}

	specBody, specURL, err := fetchBeaconEndpoint(ctx, beaconEndpoint, beaconConfigSpecPath)
	if err != nil {
		return 0, 0, err
	}
	var specResp struct {
		Data struct {
			SecondsPerSlot string `json:"SECONDS_PER_SLOT"`
		} `json:"data"`
	}
	if err := json.Unmarshal(specBody, &specResp); err != nil {
		return 0, 0, fmt.Errorf("decode beacon spec response from %s: %w", specURL, err)
	}

	if specResp.Data.SecondsPerSlot == "" {
		return 0, 0, fmt.Errorf("beacon spec response from %s missing SECONDS_PER_SLOT", specURL)
	}
	slotSeconds, err := strconv.ParseInt(specResp.Data.SecondsPerSlot, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse SECONDS_PER_SLOT %q from %s: %w", specResp.Data.SecondsPerSlot, specURL, err)
	}
	if slotSeconds <= 0 {
		return 0, 0, fmt.Errorf("beacon spec response from %s has non-positive SECONDS_PER_SLOT %q", specURL, specResp.Data.SecondsPerSlot)
	}

	genesisUnix := genesisResp.Data.GenesisTime.Unix()
	if genesisUnix < 0 {
		return 0, 0, fmt.Errorf("beacon genesis time is before unix epoch: %s", genesisResp.Data.GenesisTime)
	}

	return uint64(genesisUnix), time.Duration(slotSeconds) * time.Second, nil
}

// BeaconBlobSidecars returns the blob sidecars stored in the beacon API for a slot.
func BeaconBlobSidecars(ctx context.Context, beaconEndpoint string, slot uint64) ([]*deneb.BlobSidecar, error) {
	body, url, err := fetchBeaconEndpoint(ctx, beaconEndpoint, fmt.Sprintf(beaconBlobSidecarsPath, slot))
	if err != nil {
		return nil, err
	}

	var resp beaconBlobSidecarsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode beacon blob sidecars response from %s: %w", url, err)
	}

	return resp.Data, nil
}
