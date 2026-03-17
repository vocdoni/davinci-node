package service

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	leanimt "github.com/vocdoni/lean-imt-go"
	leancensus "github.com/vocdoni/lean-imt-go/census"
)

func TestCensusDownloaderStalledDownloadDoesNotBlockQueue(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := storage.New(memdb.New())
	c.Cleanup(store.Close)

	downloader := NewCensusDownloader(nil, store, CensusDownloaderConfig{
		CleanUpInterval:      time.Minute,
		OnchainCheckInterval: time.Minute,
		Expiration:           time.Minute,
		Cooldown:             10 * time.Millisecond,
		Attempts:             1,
		AttemptTimeout:       100 * time.Millisecond,
	})
	c.Assert(downloader.Start(ctx), qt.IsNil)
	c.Cleanup(downloader.Stop)

	stalledServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	c.Cleanup(stalledServer.Close)

	readyDump, readyRoot := testJSONDump(c)
	readyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readyDump)
	}))
	c.Cleanup(readyServer.Close)

	stalledCensus := &types.Census{
		CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		CensusRoot:   types.HexBytes{0x01},
		CensusURI:    stalledServer.URL,
	}
	readyCensus := &types.Census{
		CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		CensusRoot:   readyRoot,
		CensusURI:    readyServer.URL,
	}

	_, err := downloader.DownloadCensus(stalledCensus)
	c.Assert(err, qt.IsNil)
	_, err = downloader.DownloadCensus(readyCensus)
	c.Assert(err, qt.IsNil)

	readyDownloaded := make(chan error, 1)
	downloader.OnCensusDownloaded(readyCensus, ctx, func(err error) {
		readyDownloaded <- err
	})

	select {
	case err := <-readyDownloaded:
		c.Assert(err, qt.IsNil)
	case <-ctx.Done():
		c.Fatal("timed out waiting for ready census download")
	}

	c.Assert(store.CensusDB().ExistsByRoot(readyRoot), qt.IsTrue)
}

func testJSONDump(c *qt.C) ([]byte, types.HexBytes) {
	c.Helper()

	tree, err := leancensus.NewCensusIMT(nil, leanimt.PoseidonHasher)
	c.Assert(err, qt.IsNil)

	weight := big.NewInt(1)
	c.Assert(tree.Add(testutil.RandomAddress(), weight), qt.IsNil)

	dump, err := tree.DumpAll()
	c.Assert(err, qt.IsNil)

	dumpJSON, err := json.Marshal(dump)
	c.Assert(err, qt.IsNil)

	return dumpJSON, types.HexBytes(dump.Root.Bytes())
}
