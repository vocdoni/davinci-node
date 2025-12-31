package census

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/census/test"
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

		err = gi.DownloadAndImportCensus(
			c.Context(),
			censusDB,
			censusURI,
			test.DefaultExpectedRoot,
		)
		c.Assert(err, qt.IsNil)
	})
}
