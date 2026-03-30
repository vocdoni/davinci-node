package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/workers"
)

func TestEncodeWithParams(t *testing.T) {
	c := qt.New(t)
	testSeed := "test-seed"
	workerName := "test-worker"
	workerAddr := fmt.Sprintf("0x%x", util.RandomHex(20))

	mainAPIUUID, err := workers.WorkerSeedToUUID(testSeed)
	c.Assert(err, qt.IsNil)

	getJobEndpoint := EndpointWithParam(WorkersEndpoint, SequencerUUIDURLParam, mainAPIUUID.String())
	getJobEndpoint = EndpointWithParam(getJobEndpoint, WorkerNameQueryParam, workerName)
	getJobEndpoint = EndpointWithParam(getJobEndpoint, WorkerAddressQueryParam, workerAddr)

	t.Log(getJobEndpoint)
}

func TestStaticHandlerBlocksPathTraversal(t *testing.T) {
	c := qt.New(t)
	req := httptest.NewRequest(http.MethodGet, "/app../go.mod", nil)
	rr := httptest.NewRecorder()

	staticHandler(rr, req)

	c.Assert(rr.Code, qt.Equals, http.StatusNotFound)
}

func TestStaticHandlerServesWebappFile(t *testing.T) {
	c := qt.New(t)
	if _, err := os.Stat(path.Join(webappdir, "index.html")); err != nil {
		t.Skipf("missing webapp index file: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/app/index.html", nil)
	rr := httptest.NewRecorder()

	staticHandler(rr, req)

	c.Assert(rr.Code, qt.Equals, http.StatusOK)
}
