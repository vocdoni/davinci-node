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

		_, err = gi.ImportCensus(
			c.Context(),
			censusDB,
			&types.Census{
				ContractAddress: testutil.RandomAddress(),
				CensusURI:       censusURI,
				CensusRoot:      test.DefaultExpectedRoot,
			},
			0,
		)
		c.Assert(err, qt.IsNil)
	})
}
