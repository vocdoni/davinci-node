package census

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/census/test"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestGraphQLDownloadAndImportCensus(t *testing.T) {
	c := qt.New(t)

	censusDB := testNewCensusDB(c)
	gi := GraphQLImporter(&GraphQLImporterConfig{
		Insecure: true,
	})

	testServer := test.NewTestGraphQLServer(c.Context())
	testServer.SetEvents(test.DefaultGraphQLEvents)
	testServer.Start()
	defer testServer.Stop()

	c.Run("success", func(c *qt.C) {
		censusURI, err := testServer.GraphQLEndpoint()
		c.Assert(err, qt.IsNil)
		chainID := uint64(11155111)
		contractAddress := testutil.RandomAddress()

		_, err = gi.ImportCensus(
			c.Context(),
			censusDB,
			chainID,
			&types.Census{
				ContractAddress: contractAddress,
				CensusURI:       censusURI,
				CensusRoot:      test.DefaultExpectedRoot,
			},
			0,
		)
		c.Assert(err, qt.IsNil)

		ref, err := censusDB.LoadByScopedAddress(chainID, contractAddress)
		c.Assert(err, qt.IsNil)
		c.Assert(ref, qt.IsNotNil)
	})
}
