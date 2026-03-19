package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/vocdoni/davinci-node/types"
)

// uploadResponse is an internal struct to parse Pinata responses after file
// upload
type uploadResponse struct {
	Data struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		CID  string `json:"cid"`
		Size int64  `json:"size"`
	} `json:"data"`
}

// PinataMetadataProviderConfig is the configuration for the
// PinataMetadataProvider. It includes the hostname URL, JWT and gateway URL
// and token.
type PinataMetadataProviderConfig struct {
	HostnameURL  string
	HostnameJWT  string
	GatewayURL   string
	GatewayToken string
}

// Valid checks if the PinataMetadataProviderConfig is valid. It returns true
// if the hostname URL, JWT and gateway URL and token are not empty.
func (c *PinataMetadataProviderConfig) Valid() bool {
	return c.HostnameURL != "" && c.HostnameJWT != "" && c.GatewayURL != "" && c.GatewayToken != ""
}

// PinataMetadataProvider is a provider for metadata stored in Pinata.
type PinataMetadataProvider struct {
	PinataMetadataProviderConfig
	httpClient *http.Client
}

// NewPinataMetadataProvider creates a new PinataMetadataProvider instance with
// the given configuration.
func NewPinataMetadataProvider(config PinataMetadataProviderConfig) *PinataMetadataProvider {
	return &PinataMetadataProvider{
		PinataMetadataProviderConfig: config,
		httpClient:                   &http.Client{},
	}
}

// SetMetadata stores the given metadata in Pinata. It returns an error if the
// request fails. It ensures that the resulting CID matches with the key
// provided.
func (p *PinataMetadataProvider) SetMetadata(ctx context.Context, key types.HexBytes, metadata *types.Metadata) error {
	// Encode the metadata
	_, data, err := CID(metadata)
	if err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}
	// Write the metadata to the request
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// Create the form file
	part, err := writer.CreateFormFile("file", "metadata.json")
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	// Write the data
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("write multipart content: %w", err)
	}
	// Mark it as public file
	if err := writer.WriteField("network", "public"); err != nil {
		return fmt.Errorf("write network field: %w", err)
	}
	// Close the writer
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}
	// Create the request with the body
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.HostnameURL, &body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	// Add the JWT to the headers
	req.Header.Set("Authorization", "Bearer "+p.HostnameJWT)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	// Make the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Read the response
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	// Ensure the status is ok
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("pinata upload failed: status=%s body=%s", resp.Status, string(raw))
	}
	// Decode the response
	var out uploadResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	// Ensure the CID matches
	remoteKey := CIDStringToHexBytes(out.Data.CID)
	if !key.Equal(remoteKey) {
		return fmt.Errorf("key mismatch: expected %s, got %s", key.Hex(), remoteKey.Hex())
	}
	return nil
}

// Metadata returns the metadata stored in Pinata for the given key. It returns
// an error if the request fails.
func (p *PinataMetadataProvider) Metadata(ctx context.Context, key types.HexBytes) (*types.Metadata, error) {
	gatewayURL, err := p.gatewayURLFromKey(key)
	if err != nil {
		return nil, err
	}
	// Create the request to the gateway
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gatewayURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	// Make the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateway request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Read the response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gateway response: %w", err)
	}
	// Ensure the status is ok
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("gateway fetch failed: status=%s body=%s", resp.Status, string(data))
	}
	// Decode the response to types.Metadata
	var metadata types.Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	// Return the metadata
	return &metadata, nil
}

// gatewayURLFromKey method return the Pinata Gateway URL for a given metadata
// key. It support the following formats:
//   - https://gateway.pinata.cloud/ipfs
//   - https://gateway.pinata.cloud
//   - gateway.pinata.cloud
//
// It returns an error if the URL is invalid.
func (p *PinataMetadataProvider) gatewayURLFromKey(key types.HexBytes) (string, error) {
	// Convert the key to a CID
	c, err := HexBytesToCID(key)
	if err != nil {
		return "", fmt.Errorf("invalid cid bytes: %w", err)
	}
	// Parse GatewayURL to support both full base URLs
	// (e.g. https://gateway.pinata.cloud/ipfs) and legacy hostname-only
	// values (e.g. gateway.pinata.cloud).
	parsed, err := url.Parse(p.GatewayURL)
	if err != nil {
		return "", fmt.Errorf("invalid gateway URL %q: %w", p.GatewayURL, err)
	}
	var u url.URL
	if parsed.Scheme != "" && parsed.Host != "" {
		// Full base URL: preserve scheme/host/path and append the CID.
		u = *parsed
		basePath := strings.TrimRight(parsed.Path, "/")
		// If the base path doesn't already end with "ipfs", add it before appending the CID.
		if !strings.HasSuffix(basePath, "ipfs") {
			basePath = path.Join(basePath, "ipfs")
		}
		u.Path = path.Join(basePath, c.String())
	} else {
		// Hostname-only configuration: preserve existing behavior.
		u = url.URL{
			Scheme: "https",
			Host:   p.GatewayURL,
			Path:   "/ipfs/" + c.String(),
		}
	}
	// Add the gateway token to the URL
	q := u.Query()
	q.Set("pinataGatewayToken", p.GatewayToken)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
