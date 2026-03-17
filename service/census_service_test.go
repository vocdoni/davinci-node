package service

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
		Attempts:             5,
		AttemptTimeout:       time.Second,
		ConcurrentDownloads:  2,
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

	deadline := time.After(500 * time.Millisecond)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			c.Fatal("ready census download waited for stalled retries")
		case <-ctx.Done():
			c.Fatal("timed out waiting for ready census download")
		case <-ticker.C:
			if store.CensusDB().ExistsByRoot(readyRoot) {
				return
			}
		}
	}
}

func TestOnCensusDownloadedWaitsForQueuedCensus(t *testing.T) {
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
		AttemptTimeout:       1500 * time.Millisecond,
		ConcurrentDownloads:  1,
	})
	c.Assert(downloader.Start(ctx), qt.IsNil)
	c.Cleanup(downloader.Stop)

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	c.Cleanup(slowServer.Close)

	readyDump, readyRoot := testJSONDump(c)
	readyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readyDump)
	}))
	c.Cleanup(readyServer.Close)

	slowCensus := &types.Census{
		CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		CensusRoot:   types.HexBytes{0x02},
		CensusURI:    slowServer.URL,
	}
	readyCensus := &types.Census{
		CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		CensusRoot:   readyRoot,
		CensusURI:    readyServer.URL,
	}

	_, err := downloader.DownloadCensus(slowCensus)
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
		c.Fatal("timed out waiting for queued census download")
	}

	c.Assert(store.CensusDB().ExistsByRoot(readyRoot), qt.IsTrue)
}

func TestCensusKeyUsesContractAddressForOnchainDynamicCensuses(t *testing.T) {
	c := qt.New(t)

	address := testutil.RandomAddress()
	first := &types.Census{
		CensusOrigin:    types.CensusOriginMerkleTreeOnchainDynamicV1,
		CensusRoot:      types.HexBytes{0x01},
		ContractAddress: address,
	}
	second := &types.Census{
		CensusOrigin:    types.CensusOriginMerkleTreeOnchainDynamicV1,
		CensusRoot:      types.HexBytes{0x02},
		ContractAddress: address,
	}

	c.Assert(censusKey(first), qt.Equals, address.String())
	c.Assert(censusKey(second), qt.Equals, address.String())
}

func TestCensusDownloaderNotFoundIsTerminal(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := storage.New(memdb.New())
	c.Cleanup(store.Close)

	downloader := NewCensusDownloader(nil, store, CensusDownloaderConfig{
		CleanUpInterval:      10 * time.Millisecond,
		OnchainCheckInterval: time.Minute,
		Expiration:           10 * time.Millisecond,
		Cooldown:             10 * time.Millisecond,
		Attempts:             5,
		AttemptTimeout:       time.Second,
		ConcurrentDownloads:  1,
	})
	c.Assert(downloader.Start(ctx), qt.IsNil)
	c.Cleanup(downloader.Stop)

	var requests atomic.Int32
	notFoundServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	c.Cleanup(notFoundServer.Close)

	census := &types.Census{
		CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		CensusRoot:   types.HexBytes{0x03},
		CensusURI:    notFoundServer.URL,
	}

	_, err := downloader.DownloadCensus(census)
	c.Assert(err, qt.IsNil)

	downloaded := make(chan error, 1)
	downloader.OnCensusDownloaded(census, ctx, func(err error) {
		downloaded <- err
	})

	select {
	case err := <-downloaded:
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "404")
	case <-ctx.Done():
		c.Fatal("timed out waiting for terminal 404 error")
	}

	status, exists := downloader.DownloadCensusStatus(census)
	c.Assert(exists, qt.IsTrue)
	c.Assert(status.Terminal, qt.IsTrue)
	c.Assert(requests.Load(), qt.Equals, int32(1))

	time.Sleep(20 * time.Millisecond)
	downloader.cleanUpPendingCensuses()

	_, err = downloader.DownloadCensus(census)
	c.Assert(err, qt.IsNil)
	time.Sleep(50 * time.Millisecond)
	c.Assert(requests.Load(), qt.Equals, int32(1))
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
