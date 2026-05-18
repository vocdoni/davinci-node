package census

import (
	"context"
	"fmt"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/census/censusdb"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

const testCensusURI = "https://example.invalid/dump"

func testNewCensusDB(c *qt.C) *censusdb.CensusDB {
	c.Helper()
	internalDB, err := metadb.New(db.TypeInMem, "")
	c.Assert(err, qt.IsNil)
	return censusdb.NewCensusDB(internalDB)
}

type testImporterPlugin struct {
	validFn func(string) bool
	err     error

	calls        int
	lastChainID  uint64
	lastCensusDB *censusdb.CensusDB
	lastURI      string
	lastRoot     types.HexBytes
}

func (p *testImporterPlugin) ValidURI(targetURI string) bool {
	if p.validFn == nil {
		return false
	}
	return p.validFn(targetURI)
}

func (p *testImporterPlugin) ImportCensus(_ context.Context, censusDB *censusdb.CensusDB, chainID uint64, census *types.Census, _ int) (int, error) {
	p.calls++
	p.lastChainID = chainID
	p.lastCensusDB = censusDB
	p.lastURI = census.CensusURI
	p.lastRoot = census.CensusRoot
	return 100, p.err
}

func testNewStorage(c *qt.C) *storage.Storage {
	c.Helper()

	internalDB := metadb.NewTest(c)
	stg := storage.New(internalDB)
	c.Cleanup(stg.Close)
	return stg
}

func TestCensusImporter(t *testing.T) {
	c := qt.New(t)
	stg := testNewStorage(c)

	c.Run("Validation", func(c *qt.C) {
		c.Run("NilCensus", func(c *qt.C) {
			importer := NewCensusImporter(nil)
			_, err := importer.ImportCensus(c.Context(), 0, nil, 0)
			c.Assert(err, qt.Not(qt.IsNil))
			c.Assert(err.Error(), qt.Equals, "census is nil")
		})

		c.Run("InvalidOrigin", func(c *qt.C) {
			importer := NewCensusImporter(nil)
			_, err := importer.ImportCensus(c.Context(), 0, &types.Census{
				CensusOrigin: types.CensusOriginUnknown,
				CensusURI:    testCensusURI,
				CensusRoot:   types.HexBytes{0x01},
			}, 0)
			c.Assert(err, qt.Not(qt.IsNil))
			c.Assert(err.Error(), qt.Contains, "invalid census origin:")
		})
	})

	c.Run("MerkleTree", func(c *qt.C) {
		c.Run("DynamicOnchainRequiresChainID", func(c *qt.C) {
			plugin := &testImporterPlugin{
				validFn: func(string) bool { return true },
			}

			importer := NewCensusImporter(stg, plugin)
			_, err := importer.ImportCensus(c.Context(), 0, &types.Census{
				CensusOrigin:    types.CensusOriginMerkleTreeOnchainDynamicV1,
				CensusURI:       "graphql://example.invalid/subgraph",
				CensusRoot:      types.HexBytes{0x01},
				ContractAddress: testutil.RandomAddress(),
			}, 0)
			c.Assert(err, qt.Not(qt.IsNil))
			c.Assert(err.Error(), qt.Contains, "chain ID is required")
			c.Assert(plugin.calls, qt.Equals, 0)
		})

		c.Run("SelectsFirstMatchingPlugin", func(c *qt.C) {
			plugin1 := &testImporterPlugin{
				validFn: func(string) bool { return false },
			}
			plugin2 := &testImporterPlugin{
				validFn: func(uri string) bool { return uri == testCensusURI },
			}

			importer := NewCensusImporter(stg, plugin1, plugin2)
			census := &types.Census{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
				CensusURI:    testCensusURI,
				CensusRoot:   types.HexBytes{0xaa, 0xbb},
			}

			_, err := importer.ImportCensus(c.Context(), 0, census, 0)
			c.Assert(err, qt.IsNil)

			c.Assert(plugin1.calls, qt.Equals, 0)
			c.Assert(plugin2.calls, qt.Equals, 1)
			c.Assert(plugin2.lastCensusDB, qt.Not(qt.IsNil))
			c.Assert(plugin2.lastCensusDB, qt.Equals, stg.CensusDB())
			c.Assert(plugin2.lastChainID, qt.Equals, uint64(0))
			c.Assert(plugin2.lastURI, qt.Equals, census.CensusURI)
			c.Assert(plugin2.lastRoot, qt.DeepEquals, census.CensusRoot)
		})

		c.Run("PluginErrorPropagates", func(c *qt.C) {
			sentinelErr := fmt.Errorf("boom")
			plugin := &testImporterPlugin{
				validFn: func(string) bool { return true },
				err:     sentinelErr,
			}

			importer := NewCensusImporter(stg, plugin)
			_, err := importer.ImportCensus(c.Context(), 0, &types.Census{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainDynamicV1,
				CensusURI:    testCensusURI,
				CensusRoot:   types.HexBytes{0x01},
			}, 0)
			c.Assert(err, qt.ErrorIs, sentinelErr)
			c.Assert(plugin.calls, qt.Equals, 1)
		})

		c.Run("NoPluginFound", func(c *qt.C) {
			plugin := &testImporterPlugin{
				validFn: func(string) bool { return false },
			}
			importer := NewCensusImporter(stg, plugin)
			_, err := importer.ImportCensus(c.Context(), 0, &types.Census{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
				CensusURI:    testCensusURI,
				CensusRoot:   types.HexBytes{0x01},
			}, 0)
			c.Assert(err, qt.Not(qt.IsNil))
			c.Assert(err.Error(), qt.Contains, "no importer plugin found for census URI")
			c.Assert(plugin.calls, qt.Equals, 0)
		})
	})

	c.Run("CSP", func(c *qt.C) {
		c.Run("NoOpAndNoPluginCalled", func(c *qt.C) {
			plugin := &testImporterPlugin{
				validFn: func(string) bool { return true },
			}
			importer := NewCensusImporter(stg, plugin)
			_, err := importer.ImportCensus(c.Context(), 0, &types.Census{
				CensusOrigin: types.CensusOriginCSPEdDSABabyJubJubV1,
				CensusURI:    "https://example.invalid/csp",
				CensusRoot:   types.HexBytes{0x01},
			}, 0)
			c.Assert(err, qt.IsNil)
			c.Assert(plugin.calls, qt.Equals, 0)
		})
	})
}
