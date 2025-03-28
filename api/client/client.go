package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
)

const (
	// HTTPGET is the method string used for calling Request()
	HTTPGET = http.MethodGet
	// HTTPPOST is the method string used for calling Request()
	HTTPPOST = http.MethodPost
	// HTTPDELETE is the method string used for calling
	HTTPDELETE = http.MethodDelete

	errCodeNot200 = "API error"

	// DefaultRetries this enables Request() to handle the situation where the server connection fails
	DefaultRetries = 3
	// DefaultTimeout is the default timeout for the HTTP client
	DefaultTimeout = 10 * time.Second
)

// HTTPclient is the Vocdoni API HTTP client.
type HTTPclient struct {
	c       *http.Client
	host    *url.URL
	retries int
}

// New connects to the API host with a random bearer token and returns the handle
func New(host string) (*HTTPclient, error) {
	hostURL, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	tr := &http.Transport{
		IdleConnTimeout:    DefaultTimeout,
		DisableCompression: false,
		WriteBufferSize:    1 * 1024 * 1024, // 1 MiB
		ReadBufferSize:     1 * 1024 * 1024, // 1 MiB
	}
	c := &HTTPclient{
		c:       &http.Client{Transport: tr, Timeout: DefaultTimeout},
		host:    hostURL,
		retries: DefaultRetries,
	}
	log.Debugw("http client created", "host", hostURL.String())
	data, status, err := c.Request(HTTPGET, nil, nil, api.PingEndpoint)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s: %d (%s)", errCodeNot200, status, data)
	}
	return c, nil
}

// SetHostAddr configures the host address of the API server.
func (c *HTTPclient) SetHostAddr(host *url.URL) error {
	c.host = host
	data, status, err := c.Request(HTTPGET, nil, nil, api.PingEndpoint)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s: %d (%s)", errCodeNot200, status, data)
	}
	return nil
}

// SetRetries configures the number of retries for the HTTP client.
func (c *HTTPclient) SetRetries(n int) {
	c.retries = n
}

// SetTimeout configures the timeout for the HTTP client.
func (c *HTTPclient) SetTimeout(d time.Duration) {
	c.c.Timeout = d
	if c.c.Transport != nil {
		if _, ok := c.c.Transport.(*http.Transport); ok {
			c.c.Transport.(*http.Transport).ResponseHeaderTimeout = d
		}
	}
}

// Request performs a `method` type raw request to the endpoint specified in urlPath parameter.
// Method is either GET or POST. If POST, a JSON struct should be attached.  Returns the response,
// the status code and an error.
//
// Supports query parameters via `params` slice. If the slice is not empty, it should contain pairs of strings;
// the first element of each pair is the key, and the second element is the value.
func (c *HTTPclient) Request(method string, jsonBody any, params []string, urlPath ...string) ([]byte, int, error) {
	var (
		body []byte
		err  error
	)

	// Marshal the JSON body if provided.
	if jsonBody != nil {
		body, err = json.Marshal(jsonBody)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal JSON: %w", err)
		}
	}

	// Parse the base host URL
	u, err := url.Parse(c.host.String())
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse host URL: %w", err)
	}

	// Join path segments
	u.Path = path.Join(u.Path, path.Join(urlPath...))

	// Process query parameters from the params slice.
	// Expecting even-length slice: [key1, val1, key2, val2, ...]
	// If length is odd, the last parameter without a pair will be ignored.
	if len(params) > 0 {
		values := url.Values{}
		for i := 0; i < len(params)-1; i += 2 {
			key := params[i]
			val := params[i+1]
			values.Set(key, val)
		}
		u.RawQuery = values.Encode()
	}

	// Prepare headers
	headers := http.Header{}
	if jsonBody != nil {
		headers.Set("Content-Type", "application/json")
		headers.Set("Accept", "application/json")
	}

	// Log the request details, truncating body if large
	log.Debugw("client request",
		"type", method,
		"url", u.String(),
		"body", func() string {
			if len(body) > 512 {
				return string(body[:512]) + "..."
			}
			return strings.ReplaceAll(string(body), "\"", "")
		}(),
	)

	var resp *http.Response
	for i := 1; i <= c.retries; i++ {
		// Create a fresh request each attempt
		var reqBody io.ReadCloser
		if body != nil {
			reqBody = io.NopCloser(bytes.NewReader(body))
		}
		req, err := http.NewRequest(method, u.String(), reqBody)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header = headers

		resp, err = c.c.Do(req)
		if err != nil {
			log.Warnw("http request failed", "error", err.Error(), "attempt", i, "retries", c.retries)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Successfully got a response, break out of the retry loop
		break
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warnw("failed to close response body", "error", err)
		}
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return data, resp.StatusCode, nil
}
