package census

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/census/test"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
	leancensus "github.com/vocdoni/lean-imt-go/census"
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
	censusURI, err := testServer.GraphQLEndpoint()
	c.Assert(err, qt.IsNil)

	c.Run("success", func(c *qt.C) {
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

	c.Run("rebuilds census from full history when updates arrive out of order", func(c *qt.C) {
		contractAddress := testutil.RandomAddress()
		initialEvents := []test.TestWeightChangeEvent{
			{
				AccountID:      "0x1111111111111111111111111111111111111111",
				PreviousWeight: "0",
				NewWeight:      "1",
			},
			{
				AccountID:      "0x3333333333333333333333333333333333333333",
				PreviousWeight: "0",
				NewWeight:      "1",
			},
		}
		fullEvents := []test.TestWeightChangeEvent{
			initialEvents[0],
			{
				AccountID:      "0x2222222222222222222222222222222222222222",
				PreviousWeight: "0",
				NewWeight:      "1",
			},
			initialEvents[1],
		}

		initialRoot := testGraphQLRoot(c, initialEvents)
		fullRoot := testGraphQLRoot(c, fullEvents)

		testServer.SetEvents(initialEvents)
		processedElements, err := gi.ImportCensus(
			c.Context(),
			censusDB,
			&types.Census{
				ContractAddress: contractAddress,
				CensusURI:       censusURI,
				CensusRoot:      initialRoot,
			},
			0,
		)
		c.Assert(err, qt.IsNil)
		c.Assert(processedElements, qt.Equals, len(initialEvents))

		testServer.SetEvents(fullEvents)
		processedElements, err = gi.ImportCensus(
			c.Context(),
			censusDB,
			&types.Census{
				ContractAddress: contractAddress,
				CensusURI:       censusURI,
				CensusRoot:      fullRoot,
			},
			processedElements,
		)
		c.Assert(err, qt.IsNil)
		c.Assert(processedElements, qt.Equals, len(fullEvents))

		ref, err := censusDB.LoadByAddress(contractAddress)
		c.Assert(err, qt.IsNil)
		c.Assert(ref.Root(), qt.DeepEquals, fullRoot)
		c.Assert(ref.Size(), qt.Equals, 3)
	})
}

func testGraphQLRoot(c *qt.C, events []test.TestWeightChangeEvent) types.HexBytes {
	c.Helper()

	censusDB := testNewCensusDB(c)
	ref, err := censusDB.New(uuid.New())
	c.Assert(err, qt.IsNil)
	c.Cleanup(func() {
		if ref.Tree() != nil {
			_ = ref.Tree().Close()
		}
	})

	err = ref.ApplyEvents(testGraphQLCensusEvents(c, events))
	c.Assert(err, qt.IsNil)
	return ref.Root()
}

func testGraphQLCensusEvents(c *qt.C, events []test.TestWeightChangeEvent) []leancensus.CensusEvent {
	c.Helper()

	censusEvents := make([]leancensus.CensusEvent, 0, len(events))
	for _, event := range events {
		previousWeight, ok := new(big.Int).SetString(event.PreviousWeight, 10)
		c.Assert(ok, qt.IsTrue)

		newWeight, ok := new(big.Int).SetString(event.NewWeight, 10)
		c.Assert(ok, qt.IsTrue)

		censusEvents = append(censusEvents, leancensus.CensusEvent{
			Address:    common.HexToAddress(event.AccountID),
			PrevWeight: previousWeight,
			NewWeight:  newWeight,
		})
	}
	return censusEvents
}
