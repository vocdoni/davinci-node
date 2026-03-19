package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type errReadCloser struct {
	err error
}

func (e errReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e errReadCloser) Close() error {
	return nil
}

func newTestHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func newTestPinataProvider() *PinataMetadataProvider {
	return NewPinataMetadataProvider(PinataMetadataProviderConfig{
		HostnameURL:  "https://pinata.example/upload",
		HostnameJWT:  "jwt-token",
		GatewayURL:   "gateway.example",
		GatewayToken: "gateway-token",
	})
}

func TestPinataMetadataProviderConfigValid(t *testing.T) {
	c := qt.New(t)

	cases := []struct {
		name   string
		config PinataMetadataProviderConfig
		valid  bool
	}{
		{
			name: "valid without hostname url",
			config: PinataMetadataProviderConfig{
				HostnameJWT:  "jwt",
				GatewayURL:   "gateway.example",
				GatewayToken: "token",
			},
			valid: true,
		},
		{
			name: "missing jwt",
			config: PinataMetadataProviderConfig{
				GatewayURL:   "gateway.example",
				GatewayToken: "token",
			},
			valid: false,
		},
		{
			name: "missing gateway url",
			config: PinataMetadataProviderConfig{
				HostnameJWT:  "jwt",
				GatewayToken: "token",
			},
			valid: false,
		},
		{
			name: "missing gateway token",
			config: PinataMetadataProviderConfig{
				HostnameJWT: "jwt",
				GatewayURL:  "gateway.example",
			},
			valid: false,
		},
	}

	for _, tc := range cases {
		c.Run(tc.name, func(c *qt.C) {
			c.Assert(tc.config.Valid(), qt.Equals, tc.valid)
		})
	}
}

func TestPinataMetadataProviderSetMetadata(t *testing.T) {
	c := qt.New(t)

	metadata := testMetadata()
	key, expectedJSON, err := CID(metadata)
	c.Assert(err, qt.IsNil)

	c.Run("success", func(c *qt.C) {
		provider := newTestPinataProvider()
		provider.httpClient = newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			c.Assert(req.Method, qt.Equals, http.MethodPost)
			c.Assert(req.URL.String(), qt.Equals, provider.HostnameURL)
			c.Assert(req.Header.Get("Authorization"), qt.Equals, "Bearer "+provider.HostnameJWT)
			c.Assert(req.Header.Get("Content-Type"), qt.Contains, "multipart/form-data")

			reader, err := req.MultipartReader()
			c.Assert(err, qt.IsNil)

			parts := map[string]string{}
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				c.Assert(err, qt.IsNil)

				body, err := io.ReadAll(part)
				c.Assert(err, qt.IsNil)

				name := part.FormName()
				if part.FileName() != "" {
					c.Assert(name, qt.Equals, "file")
					c.Assert(part.FileName(), qt.Equals, "metadata.json")
					c.Assert(body, qt.DeepEquals, expectedJSON)
				} else {
					parts[name] = string(body)
				}
			}
			c.Assert(parts["network"], qt.Equals, "public")

			respBody := fmt.Sprintf(`{"data":{"cid":"%s"}}`, mustCIDString(c, key))
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(respBody)),
			}, nil
		})

		err := provider.SetMetadata(context.Background(), key, metadata)
		c.Assert(err, qt.IsNil)
	})

	c.Run("success with default client", func(c *qt.C) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			c.Assert(req.Method, qt.Equals, http.MethodPost)
			c.Assert(req.Header.Get("Authorization"), qt.Equals, "Bearer default-client-jwt")
			_, err = fmt.Fprintf(w, `{"data":{"cid":"%s"}}`, mustCIDString(c, key))
			c.Assert(err, qt.IsNil)
		}))
		defer server.Close()

		provider := NewPinataMetadataProvider(PinataMetadataProviderConfig{
			HostnameURL:  server.URL,
			HostnameJWT:  "default-client-jwt",
			GatewayURL:   "gateway.example",
			GatewayToken: "gateway-token",
		})

		err := provider.SetMetadata(context.Background(), key, metadata)
		c.Assert(err, qt.IsNil)
	})

	c.Run("encode failure", func(c *qt.C) {
		provider := newTestPinataProvider()
		err := provider.SetMetadata(context.Background(), key, testUnsupportedMetadata())
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "encode metadata")
	})

	c.Run("transport failure", func(c *qt.C) {
		provider := newTestPinataProvider()
		expectedErr := fmt.Errorf("transport failed")
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return nil, expectedErr
		})

		err := provider.SetMetadata(context.Background(), key, metadata)
		c.Assert(err, qt.ErrorIs, expectedErr)
		c.Assert(err.Error(), qt.Contains, "upload request failed")
	})

	c.Run("response read failure", func(c *qt.C) {
		provider := newTestPinataProvider()
		expectedErr := fmt.Errorf("read failed")
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       errReadCloser{err: expectedErr},
			}, nil
		})

		err := provider.SetMetadata(context.Background(), key, metadata)
		c.Assert(err, qt.ErrorIs, expectedErr)
		c.Assert(err.Error(), qt.Contains, "read response")
	})

	c.Run("non-2xx response", func(c *qt.C) {
		provider := newTestPinataProvider()
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Status:     "502 Bad Gateway",
				Body:       io.NopCloser(strings.NewReader("nope")),
			}, nil
		})

		err := provider.SetMetadata(context.Background(), key, metadata)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "pinata upload failed")
		c.Assert(err.Error(), qt.Contains, "502 Bad Gateway")
		c.Assert(err.Error(), qt.Contains, "nope")
	})

	c.Run("invalid json response", func(c *qt.C) {
		provider := newTestPinataProvider()
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("not-json")),
			}, nil
		})

		err := provider.SetMetadata(context.Background(), key, metadata)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "decode response")
	})

	c.Run("cid mismatch", func(c *qt.C) {
		provider := newTestPinataProvider()
		otherKey, _, err := CID(&types.Metadata{Version: "different"})
		c.Assert(err, qt.IsNil)
		respBody := fmt.Sprintf(`{"data":{"cid":"%s"}}`, mustCIDString(c, otherKey))
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(respBody)),
			}, nil
		})

		err = provider.SetMetadata(context.Background(), key, metadata)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "key mismatch")
	})
}

func TestPinataMetadataProviderMetadata(t *testing.T) {
	c := qt.New(t)

	metadata := testMetadata()
	key, _, err := CID(metadata)
	c.Assert(err, qt.IsNil)
	cidString := mustCIDString(c, key)

	c.Run("success", func(c *qt.C) {
		provider := newTestPinataProvider()
		provider.httpClient = newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			c.Assert(req.Method, qt.Equals, http.MethodGet)
			c.Assert(req.URL.Scheme, qt.Equals, "https")
			c.Assert(req.URL.Host, qt.Equals, provider.GatewayURL)
			c.Assert(req.URL.Path, qt.Equals, "/ipfs/"+cidString)
			c.Assert(req.URL.Query().Get("pinataGatewayToken"), qt.Equals, provider.GatewayToken)

			body, err := json.Marshal(metadata)
			c.Assert(err, qt.IsNil)
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		})

		got, err := provider.Metadata(context.Background(), key)
		c.Assert(err, qt.IsNil)
		c.Assert(got, qt.DeepEquals, metadata)
	})

	c.Run("invalid cid bytes", func(c *qt.C) {
		provider := newTestPinataProvider()
		got, err := provider.Metadata(context.Background(), types.HexBytes("not-a-cid"))
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "invalid cid bytes")
	})

	c.Run("transport failure", func(c *qt.C) {
		provider := newTestPinataProvider()
		expectedErr := fmt.Errorf("transport failed")
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return nil, expectedErr
		})

		got, err := provider.Metadata(context.Background(), key)
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.ErrorIs, expectedErr)
		c.Assert(err.Error(), qt.Contains, "gateway request failed")
	})

	c.Run("response read failure", func(c *qt.C) {
		provider := newTestPinataProvider()
		expectedErr := fmt.Errorf("read failed")
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       errReadCloser{err: expectedErr},
			}, nil
		})

		got, err := provider.Metadata(context.Background(), key)
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.ErrorIs, expectedErr)
		c.Assert(err.Error(), qt.Contains, "read gateway response")
	})

	c.Run("non-2xx response", func(c *qt.C) {
		provider := newTestPinataProvider()
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     "401 Unauthorized",
				Body:       io.NopCloser(strings.NewReader("forbidden")),
			}, nil
		})

		got, err := provider.Metadata(context.Background(), key)
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "gateway fetch failed")
		c.Assert(err.Error(), qt.Contains, "401 Unauthorized")
		c.Assert(err.Error(), qt.Contains, "forbidden")
	})

	c.Run("invalid json response", func(c *qt.C) {
		provider := newTestPinataProvider()
		provider.httpClient = newTestHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("not-json")),
			}, nil
		})

		got, err := provider.Metadata(context.Background(), key)
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "decode response")
	})
}

func mustCIDString(c *qt.C, key types.HexBytes) string {
	cidValue, err := HexBytesToCID(key)
	c.Assert(err, qt.IsNil)
	return cidValue.String()
}
