package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/workers"
)

func TestAPI_matchesSequencerUUID(t *testing.T) {
	c := qt.New(t)

	sequencerUUID := uuid.New()
	api := &API{
		sequencerUUID: &sequencerUUID,
	}

	c.Assert(api.matchesSequencerUUID(sequencerUUID.String()), qt.IsTrue)
	c.Assert(api.matchesSequencerUUID(uuid.New().String()), qt.IsFalse)
	c.Assert(api.matchesSequencerUUID("not-a-uuid"), qt.IsFalse)
	c.Assert(api.matchesSequencerUUID(strings.Repeat("a", 4096)), qt.IsFalse)
}

func apiStorageForTest(t *testing.T) *storage.Storage {
	c := qt.New(t)
	tempDir := t.TempDir()
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	testDB, err := metadb.New(db.TypePebble, filepath.Join(tempDir, "db"))
	c.Assert(err, qt.IsNil)

	store := storage.New(testDB)
	t.Cleanup(func() {
		store.Close()
	})
	return store
}

func TestWorkersSubmitJobBodyTooLarge(t *testing.T) {
	c := qt.New(t)

	store := apiStorageForTest(t)
	sequencerSigner, err := ethereum.NewSigner()
	c.Assert(err, qt.IsNil)
	workerSigner, err := ethereum.NewSigner()
	c.Assert(err, qt.IsNil)
	sequencerUUID := uuid.New()

	jobsManager := workers.NewJobsManager(store, time.Minute, nil)
	jobsManager.WorkerManager.AddWorker(workerSigner.Address().Hex(), "worker-1")

	api := &API{
		storage:                    store,
		sequencerSigner:            sequencerSigner,
		sequencerUUID:              &sequencerUUID,
		jobsManager:                jobsManager,
		workersAuthtokenExpiration: time.Minute,
		parentCtx:                  context.Background(),
	}

	signMsg, createdAt, _ := workers.WorkerAuthTokenData(sequencerSigner.Address(), time.Now())
	signature, err := workerSigner.Sign([]byte(signMsg))
	c.Assert(err, qt.IsNil)
	timestamp, err := time.Parse("2006-01-02T15:04:05.000000000Z07:00", createdAt)
	c.Assert(err, qt.IsNil)
	token, err := workers.EncodeWorkerAuthToken(signature, timestamp)
	c.Assert(err, qt.IsNil)

	body := strings.Repeat("a", 1<<20+1)
	endpoint := EndpointWithParam(WorkerJobEndpoint, SequencerUUIDURLParam, sequencerUUID.String())
	endpoint = EndpointWithParam(endpoint, WorkerAddressQueryParam, workerSigner.Address().Hex())
	endpoint = EndpointWithParam(endpoint, WorkerTokenQueryParam, token.String())
	req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(SequencerUUIDURLParam, sequencerUUID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rr := httptest.NewRecorder()

	api.workersSubmitJob(rr, req)

	c.Assert(rr.Code, qt.Equals, http.StatusRequestEntityTooLarge)
	c.Assert(rr.Body.String(), qt.Contains, "request body too large")
}
