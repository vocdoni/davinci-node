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

	"github.com/vocdoni/davinci-node/types"
)

type uploadResponse struct {
	Data struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		CID  string `json:"cid"`
		Size int64  `json:"size"`
	} `json:"data"`
}

type PinataMetadataProviderConfig struct {
	HostnameURL  string
	HostnameJWT  string
	GatewayURL   string
	GatewayToken string
}

func (c *PinataMetadataProviderConfig) Valid() bool {
	return c.HostnameJWT != "" && c.GatewayURL != "" && c.GatewayToken != ""
}

type PinataMetadataProvider struct {
	PinataMetadataProviderConfig
}

func NewPinataMetadataProvider(config PinataMetadataProviderConfig) *PinataMetadataProvider {
	return &PinataMetadataProvider{PinataMetadataProviderConfig: config}
}

func (p *PinataMetadataProvider) SetMetadata(ctx context.Context, key types.HexBytes, metadata *types.Metadata) error {
	_, data, err := CID(metadata)
	if err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "metadata.json")
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("write multipart content: %w", err)
	}
	if err := writer.WriteField("network", "public"); err != nil {
		return fmt.Errorf("write network field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.HostnameURL,
		&body,
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.HostnameJWT)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pinata upload failed: status=%s body=%s", resp.Status, string(raw))
	}

	var out uploadResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	remoteKey := CIDStringToHexBytes(out.Data.CID)
	if !key.Equal(remoteKey) {
		return fmt.Errorf("key mismatch: expected %s, got %s", key.Hex(), remoteKey.Hex())
	}
	return nil
}

func (p *PinataMetadataProvider) Metadata(ctx context.Context, key types.HexBytes) (*types.Metadata, error) {
	c, err := HexBytesToCID(key)
	if err != nil {
		return nil, fmt.Errorf("invalid cid bytes: %w", err)
	}

	u := url.URL{
		Scheme: "https",
		Host:   p.GatewayURL,
		Path:   "/ipfs/" + c.String(),
	}
	q := u.Query()
	q.Set("pinataGatewayToken", p.GatewayToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gateway response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway fetch failed: status=%s body=%s", resp.Status, string(data))
	}

	var metadata types.Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &metadata, nil
}
