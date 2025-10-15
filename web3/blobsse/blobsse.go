// Package blobsse provides a small, dependency-free SSE (Server-Sent Events) consumer
// that subscribes to Ethereum consensus-layer "blob_sidecar" events
// and fetches the corresponding blob sidecars via the Beacon (consensus) API.
//
// Key features:
//   - Auto-reconnect with exponential backoff
//   - Minimal SSE parser (text/event-stream)
//   - Deduplication by (block_id, index)
//   - Pluggable callback for each fetched blob sidecar
//
// Example usage:
//
//	ctx := context.Background()
//	c := blobsse.New("https://your-beacon-node.example", nil)
//	c.OnBlob = func(ctx context.Context, sc blobsse.Sidecar) {
//	    fmt.Printf("slot=%s index=%s commitment=%s size=%d\n",
//	       sc.BlockID, sc.Index, sc.KZGCommitment, len(sc.Blob))
//	}
//	if err := c.Start(ctx); err != nil { log.Fatal(err) }
//
// Notes:
//   - The SSE payload schema may vary by client; we try to extract block_root or slot
//     from the event's JSON and use it as {block_id} for /eth/v1/beacon/blob_sidecars/{block_id}.
//   - Block ID may be a root (0x…) or a decimal slot string.
//   - The blob bytes come hex-encoded in the REST response; we decode to raw bytes.
package blobsse

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/log"
)

// Config allows customizing the consumer.
type Config struct {
	// Topics to subscribe to. Defaults to ["blob_sidecar"].
	Topics []string
	// HTTPClient to use. Defaults to http.DefaultClient.
	HTTPClient *http.Client
	// Backoff parameters for SSE reconnects.
	BackoffInitial time.Duration // default 1s
	BackoffMax     time.Duration // default 30s
	// Dedup window (how long to remember seen (blockID,index)). Default 10 minutes.
	DedupWindow time.Duration
}

// Sidecar represents one blob sidecar returned by the beacon API.
// Fields are strings to avoid type pitfalls across clients; Blob is decoded bytes.
type Sidecar struct {
	BlockID       string // slot or block root used as {block_id}
	Index         string `json:"index"`
	KZGCommitment string `json:"kzg_commitment"`
	KZGProof      string `json:"kzg_proof"`
	BlobHex       string `json:"blob"` // hex string in API, raw Blob is decoded
	Blob          []byte // decoded from BlobHex
}

// Client consumes SSE events and fetches blob sidecars.
type Client struct {
	beaconAPI string
	cfg       Config

	// OnBlob is called for every fetched sidecar (already hex-decoded).
	OnBlob func(ctx context.Context, sc Sidecar)

	mu      sync.Mutex
	seen    map[string]time.Time // key "blockID#index" -> timestamp
	closing chan struct{}
}

// New creates a Client.
func New(beaconAPI string, cfg *Config) *Client {
	c := &Client{beaconAPI: strings.TrimRight(beaconAPI, "/")}
	if cfg != nil {
		c.cfg = *cfg
	}
	if len(c.cfg.Topics) == 0 {
		c.cfg.Topics = []string{"blob_sidecar"}
	}
	if c.cfg.HTTPClient == nil {
		c.cfg.HTTPClient = http.DefaultClient
	}
	if c.cfg.BackoffInitial <= 0 {
		c.cfg.BackoffInitial = time.Second
	}
	if c.cfg.BackoffMax <= 0 {
		c.cfg.BackoffMax = 30 * time.Second
	}
	if c.cfg.DedupWindow <= 0 {
		c.cfg.DedupWindow = 10 * time.Minute
	}
	c.seen = make(map[string]time.Time)
	c.closing = make(chan struct{})
	return c
}

// Start begins the SSE loop and blocks until ctx is done or a fatal error occurs.
func (c *Client) Start(ctx context.Context) error {
	backoff := c.cfg.BackoffInitial
	for {

		if err := c.consumeOnce(ctx); err != nil {
			// If context canceled or closed, exit.
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				return err
			}
			// Backoff and retry.
			t := min(backoff, c.cfg.BackoffMax)
			// Optional: jitter could be added here.
			select {
			case <-time.After(t):
				if backoff < c.cfg.BackoffMax {
					backoff *= 2
				}
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		// Successful consumeOnce returns only on context cancellation; reset backoff.
		backoff = c.cfg.BackoffInitial
	}
}

// consumeOnce opens a single SSE connection and processes events until broken.
func (c *Client) consumeOnce(ctx context.Context) error {
	q := url.Values{}
	q.Set("topics", strings.Join(c.cfg.Topics, ","))
	u := fmt.Sprintf("%s/eth/v1/events?%s", c.beaconAPI, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warnw("failed to close response body", "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("SSE subscribe failed: %d %s", resp.StatusCode, string(b))
	}

	reader := bufio.NewReader(resp.Body)
	var evName string
	var dataBuf strings.Builder

	flushEvent := func() error {
		if dataBuf.Len() == 0 {
			return nil
		}
		data := dataBuf.String()
		dataBuf.Reset()
		// We only care about data payload; evName may help filter if needed.
		return c.handleEvent(ctx, evName, []byte(data))
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" { // event boundary
			if err := flushEvent(); err != nil {
				return err
			}
			evName = ""
			continue
		}
		if strings.HasPrefix(line, ":") {
			// comment/heartbeat, ignore
			continue
		}
		if after, ok := strings.CutPrefix(line, "event:"); ok {
			evName = strings.TrimSpace(after)
			continue
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(strings.TrimSpace(after))
			continue
		}
		// ignore other fields (id:, retry:, etc.)
	}
}

// handleEvent parses a blob_sidecar event, extracts block root or slot, then fetches sidecars.
func (c *Client) handleEvent(ctx context.Context, eventName string, data []byte) error {
	log.Debugf("handling event %q", eventName)
	// Event payloads vary; try to find block_root or slot.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil // skip unparseable event
	}
	// Many clients wrap payload under "message" or "data"; unwrap shallowly.
	if v, ok := m["message"]; ok {
		if mm, ok := v.(map[string]any); ok {
			m = mm
		}
	} else if v, ok := m["data"]; ok {
		if mm, ok := v.(map[string]any); ok {
			m = mm
		}
	}

	blockID := ""
	if s, _ := m["block_root"].(string); s != "" {
		blockID = s
	}
	if blockID == "" {
		// Some clients send slot as string, others as number
		if s, _ := m["slot"].(string); s != "" {
			blockID = s
		} else if f, _ := m["slot"].(float64); f != 0 {
			blockID = fmt.Sprintf("%d", int64(f))
		}
	}
	if blockID == "" {
		return nil
	}
	return c.fetchAndDispatch(ctx, blockID)
}

// fetchAndDispatch pulls /eth/v1/beacon/blob_sidecars/{block_id} and calls OnBlob for new items.
func (c *Client) fetchAndDispatch(ctx context.Context, blockID string) error {
	u := fmt.Sprintf("%s/eth/v1/beacon/blob_sidecars/%s", c.beaconAPI, url.PathEscape(blockID))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warnw("failed to close response body", "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		// 404 can happen if the event races ahead of availability; caller can ignore.
		return fmt.Errorf("sidecars fetch %s: %s", blockID, resp.Status)
	}
	var out struct {
		Data []struct {
			Index         string `json:"index"`
			Blob          string `json:"blob"`
			KZGCommitment string `json:"kzg_commitment"`
			KZGProof      string `json:"kzg_proof"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}

	c.gcSeen()
	for _, it := range out.Data {
		key := blockID + "#" + it.Index
		if c.isSeen(key) {
			continue
		}
		blob, err := hexToBytes(it.Blob)
		if err != nil {
			continue
		}
		sc := Sidecar{
			BlockID:       blockID,
			Index:         it.Index,
			KZGCommitment: it.KZGCommitment,
			KZGProof:      it.KZGProof,
			BlobHex:       it.Blob,
			Blob:          blob,
		}
		c.markSeen(key)
		if c.OnBlob != nil {
			c.OnBlob(ctx, sc)
		}
	}
	return nil
}

func hexToBytes(h string) ([]byte, error) {
	s := strings.TrimPrefix(strings.ToLower(h), "0x")
	// Some clients may return base64 for SSZ; here we expect hex per REST JSON spec.
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (c *Client) isSeen(k string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.seen[k]
	return ok
}

func (c *Client) markSeen(k string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen[k] = time.Now()
}

func (c *Client) gcSeen() {
	c.mu.Lock()
	defer c.mu.Unlock()
	cut := time.Now().Add(-c.cfg.DedupWindow)
	for k, t := range c.seen {
		if t.Before(cut) {
			delete(c.seen, k)
		}
	}
}
