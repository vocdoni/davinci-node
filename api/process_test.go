package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/metadata"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func TestSetMetadata(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	store := storage.New(testDB)
	defer store.Close()

	api := &API{
		storage:  store,
		metadata: metadata.New(metadata.CID, metadata.NewLocalMetadata(store.DB())),
	}

	t.Run("BodyTooLarge", func(t *testing.T) {
		c := qt.New(t)
		payload := `{"title":{"default":"` + strings.Repeat("a", maxMetadataBodyBytes+1) + `"}}`
		req := httptest.NewRequest(http.MethodPost, MetadataSetEndpoint, strings.NewReader(payload))
		rr := httptest.NewRecorder()

		api.setMetadata(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusRequestEntityTooLarge)
		c.Assert(rr.Body.String(), qt.Contains, "request body too large")
	})

	t.Run("ValidMetadata", func(t *testing.T) {
		c := qt.New(t)
		payload := `{"title":{"default":"election"}}`
		req := httptest.NewRequest(http.MethodPost, MetadataSetEndpoint, strings.NewReader(payload))
		rr := httptest.NewRecorder()

		api.setMetadata(rr, req)

		c.Assert(rr.Code, qt.Equals, http.StatusOK)
		resp := &SetMetadataResponse{}
		err := json.Unmarshal(rr.Body.Bytes(), resp)
		c.Assert(err, qt.IsNil)
		c.Assert(resp.Hash, qt.Not(qt.IsNil))
		c.Assert(resp.Hash, qt.Not(qt.HasLen), 0)

		storedMetadata, err := api.metadata.Get(context.Background(), resp.Hash)
		c.Assert(err, qt.IsNil)
		c.Assert(storedMetadata.Title, qt.DeepEquals, types.MultilingualString{"default": "election"})
	})
}
