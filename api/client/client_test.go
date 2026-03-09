package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
)

func TestHTTPClientConfigSetters(t *testing.T) {
	c := qt.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.PingEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	hostURL, err := url.Parse(server.URL)
	c.Assert(err, qt.IsNil)

	transport := &http.Transport{}
	httpClient := &HTTPclient{
		c:       &http.Client{Transport: transport},
		retries: DefaultRetries,
	}

	err = httpClient.SetHostAddr(hostURL)
	c.Assert(err, qt.IsNil)

	httpClient.SetRetries(7)
	httpClient.SetTimeout(3 * time.Second)

	c.Assert(httpClient.host.String(), qt.Equals, hostURL.String())
	c.Assert(httpClient.retries, qt.Equals, 7)
	c.Assert(httpClient.c.Timeout, qt.Equals, 3*time.Second)
	c.Assert(transport.ResponseHeaderTimeout, qt.Equals, 3*time.Second)
}
