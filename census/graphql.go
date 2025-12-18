package census

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/census3-bigquery/censusdb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/lean-imt-go/census"
)

// defaultQuery is the default GraphQL query to fetch weight change events.
const defaultQuery = `
	query GetWeightChangeEvents($first: Int!, $skip: Int!) {
		weightChangeEvents(
			first: $first
			skip: $skip
			orderBy: blockNumber
			orderDirection: asc
		) {
			account {
				id
			}
			previousWeight
			newWeight
		}
	}
`

// defaultConfig holds the default configuration for the GraphQL importer.
var defaultConfig = GraphQLImporterConfig{
	PageSize:     1000,
	QueryTimeout: 30 * time.Second,
}

// treeOp represents the type of operation to be performed on the Merkle tree.
type treeOp int

const (
	treeOpInsert treeOp = iota // insert leaf
	treeOpDelete               // delete leaf
	treeOpUpdate               // update leaf
	treeOpNoOp                 // no operation
)

// graphqlEvent represents a single weight change event fetched from the
// GraphQL endpoint. It contains the account address, previous weight, and
// new weight.
type graphqlEvent struct {
	Address    common.Address
	PrevWeight *big.Int
	NewWeight  *big.Int
}

// treeOp determines the type of tree operation based on the previous and new
// weights in the event:
//   - Insert: previous weight is 0, new weight > 0
//   - Delete: previous weight > 0, new weight is 0
//   - Update: previous weight > 0, new weight > 0
//
// If both weights are 0, it returns NoOp.
func (e graphqlEvent) treeOp() treeOp {
	newWeight := e.NewWeight.Int64()
	prevWeight := e.PrevWeight.Int64()

	switch {
	case prevWeight == 0 && newWeight > 0:
		return treeOpInsert
	case prevWeight > 0 && newWeight == 0:
		return treeOpDelete
	case prevWeight > 0 && newWeight > 0:
		return treeOpUpdate
	default:
		return treeOpNoOp
	}
}

// graphqlResponse represents the structure of the GraphQL response. It
// contains the data and any errors returned by the GraphQL endpoint.
type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// graphqlResponseData represents the structure of each weight change event
// in the GraphQL response data.
type graphqlResponseData struct {
	Account struct {
		ID string `json:"id"`
	} `json:"account"`
	PreviousWeight string `json:"previousWeight"`
	NewWeight      string `json:"newWeight"`
}

// toGraphQLEvent converts the graphqlResponseData to a graphqlEvent. It parses
// the account ID to an Ethereum address and the weights to big.Int. It returns
// an error if any parsing fails.
func (e graphqlResponseData) toGraphQLEvent() (graphqlEvent, error) {
	// Parse account ID to Ethereum address
	addr := common.HexToAddress(e.Account.ID)
	if addr == (common.Address{}) {
		return graphqlEvent{}, fmt.Errorf("invalid account address: %s", e.Account.ID)
	}
	// Parse previous and new weights to big.Int
	prevWeight, ok := new(big.Int).SetString(e.PreviousWeight, 10)
	if !ok {
		return graphqlEvent{}, fmt.Errorf("invalid previous weight: %s", e.PreviousWeight)
	}
	newWeight, ok := new(big.Int).SetString(e.NewWeight, 10)
	if !ok {
		return graphqlEvent{}, fmt.Errorf("invalid new weight: %s", e.NewWeight)
	}
	// Return the constructed graphqlEvent
	return graphqlEvent{
		Address:    addr,
		PrevWeight: prevWeight,
		NewWeight:  newWeight,
	}, nil
}

// GraphQLImporterConfig holds configuration options for the GraphQL importer.
type GraphQLImporterConfig struct {
	PageSize     int
	QueryTimeout time.Duration
}

// GraphQLImporter returns an instance of graphqlImporter with the provided
// configuration. If config is nil, it uses the default configuration.
func GraphQLImporter(config *GraphQLImporterConfig) graphqlImporter {
	if config == nil {
		config = &defaultConfig
	}
	return graphqlImporter{
		queryTimeout: config.QueryTimeout,
		pageSize:     config.PageSize,
	}
}

// graphqlImporter is an implementation of the ImporterPlugin interface for
// importing censuses from GraphQL endpoints.
type graphqlImporter struct {
	queryTimeout time.Duration
	pageSize     int
}

// ValidURI checks if the provided targetURI is a valid GraphQL endpoint URL
// (starts with "graphql://").
func (d *graphqlImporter) ValidURI(targetURI string) bool {
	return strings.HasPrefix(targetURI, "graphql://")
}

// DownloadAndImportCensus downloads the census merkle tree events from the
// specified targetURI and imports them into the census DB based on the
// expectedRoot. It returns an error if the download or import fails.
func (d *graphqlImporter) DownloadAndImportCensus(
	ctx context.Context,
	censusDB *censusdb.CensusDB,
	targetURI string,
	expectedRoot types.HexBytes,
) error {
	// Parse the GraphQL endpoint from the target URI
	endpoint, err := endpointFromURI(targetURI)
	if err != nil {
		return fmt.Errorf("invalid GraphQL URI: %w", err)
	}
	// Get the graphql events from the target URI
	events, err := queryEvents(ctx, endpoint, d.pageSize, d.queryTimeout)
	if err != nil {
		return fmt.Errorf("failed to query GraphQL events from %s: %w", targetURI, err)
	}
	if len(events) == 0 {
		return fmt.Errorf("empty census with endpoint: %s", targetURI)
	}
	// Create an empty census tree with the expected root
	emptyTree, err := censusDB.EmptyTreeByRoot(expectedRoot)
	if err != nil {
		return fmt.Errorf("failed to create empty census tree: %w", err)
	}
	// Update the censusRef from the events
	if err := updateCensusRefFromEvents(emptyTree, events, expectedRoot); err != nil {
		return fmt.Errorf("failed to update census from events: %w", err)
	}
	// Create a new CensusRef in the censusDB
	censusRef, err := censusDB.NewByRoot(expectedRoot)
	if err != nil {
		return fmt.Errorf("failed to create new census: %w", err)
	}
	// Set the updated tree in the censusRef
	censusRef.SetTree(emptyTree)
	return nil
}

// endpointFromURI converts a GraphQL URI (starting with "graphql://") to an
// HTTP endpoint by replacing the scheme with "http://".
func endpointFromURI(targetURI string) (string, error) {
	if !strings.HasPrefix(targetURI, "graphql://") {
		return "", fmt.Errorf("invalid GraphQL URI: %s", targetURI)
	}
	return "http://" + strings.TrimPrefix(targetURI, "graphql://"), nil
}

// queryPageBody constructs the GraphQL query body for pagination with the
// specified first and skip parameters. It returns the body as an io.Reader,
// after marshaling it to JSON, or an error if marshaling fails.
func queryPageBody(first, skip int) (io.Reader, error) {
	jsonQuery, err := json.Marshal(map[string]any{
		"query": defaultQuery,
		"variables": map[string]any{
			"first": first,
			"skip":  skip,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error encoding query: %v", err)
	}
	return bytes.NewBuffer(jsonQuery), nil
}

// queryEvents fetches weight change events from the specified GraphQL endpoint
// using pagination. It returns a slice of graphqlEvent or an error if the
// query fails. It iterates through pages until no more events are available,
// parsing the responses and accumulating the results.
func queryEvents(ctx context.Context, url string, pageSize int, timeout time.Duration) ([]graphqlEvent, error) {
	// Setup pagination variables
	first := pageSize
	skip := 0
	// Create the result slice and http client to be reused
	var results []graphqlEvent
	client := &http.Client{
		Timeout: timeout,
	}
	// Start iterating through pages
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// Build the query body
			queryBody, err := queryPageBody(first, skip)
			if err != nil {
				return nil, fmt.Errorf("error building query: %v", err)
			}
			// Execute the HTTP request with context
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, queryBody)
			if err != nil {
				return nil, fmt.Errorf("error creating request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("error executing request: %v", err)
			}
			// Ensure the response body is closed after processing
			receivedEvents := 0
			if err := func() error {
				defer func() {
					if err := res.Body.Close(); err != nil {
						log.Warnw("failed to close GraphQL response body",
							"uri", url,
							"err", err.Error())
					}
				}()
				// Check for non-200 status codes
				if res.StatusCode != http.StatusOK {
					return fmt.Errorf("non-200 response: %d", res.StatusCode)
				}
				// Decode the GraphQL response
				var gqlResp graphqlResponse
				if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
					return fmt.Errorf("error decoding response: %v", err)
				}
				if len(gqlResp.Errors) > 0 {
					return fmt.Errorf("graphql errors: %v", gqlResp.Errors)
				}
				// Decode events from response data
				var data struct {
					WeightChangeEvents []graphqlResponseData `json:"weightChangeEvents"`
				}
				if err := json.Unmarshal(gqlResp.Data, &data); err != nil {
					return fmt.Errorf("error unmarshaling data: %v", err)
				}
				receivedEvents = len(data.WeightChangeEvents)
				// Convert to graphqlEvent and append to results
				for _, item := range data.WeightChangeEvents {
					event, err := item.toGraphQLEvent()
					if err != nil {
						return fmt.Errorf("error converting event: %v", err)
					}
					results = append(results, event)
				}
				return nil
			}(); err != nil {
				return nil, err
			}
			// Check if we received less than pageSize items, indicating the
			// last page
			if receivedEvents < pageSize {
				return results, nil
			}
			// Update skip for next page
			skip += first
		}
	}
}

// updateCensusRefFromEvents updates the given censusRef by applying the
// provided graphqlEvents. It processes each event to modify the Merkle tree
// accordingly (insert, delete, update). After processing all events, it
// verifies that the final root matches the expectedRoot. It returns an error
// if any operation fails or if the final root does not match.
func updateCensusRefFromEvents(
	tree *census.CensusIMT,
	events []graphqlEvent,
	expectedRoot types.HexBytes,
) error {
	// Iterate over events to set the leaves
	for _, event := range events {
		switch event.treeOp() {
		case treeOpInsert:
			if err := tree.Add(event.Address, event.NewWeight); err != nil {
				return fmt.Errorf("failed to insert address %s: %w", event.Address.Hex(), err)
			}
		case treeOpDelete:
			// CRITICAL: tree.Update(index, 0) sets the leaf to 0 but KEEPS the
			// slot. The tree size doesn't decrease, it maintains an empty slot
			// at that index.
			if err := tree.Update(event.Address, big.NewInt(0)); err != nil {
				return fmt.Errorf("failed to delete address %s: %w", event.Address.Hex(), err)
			}
		case treeOpUpdate:
			if err := tree.Update(event.Address, event.NewWeight); err != nil {
				return fmt.Errorf("failed to update address %s: %w", event.Address.Hex(), err)
			}
		case treeOpNoOp:
			// No operation needed
		}
	}
	// Compute the final root of the tree after processing all events
	treeRoot, ok := tree.Root()
	if !ok {
		return fmt.Errorf("failed to compute final census root")
	}
	finalRoot := types.HexBytes(treeRoot.Bytes())
	// Verify the final root matches the expected root
	if !bytes.Equal(finalRoot, expectedRoot) {
		return fmt.Errorf("final census root mismatch: expected %x, got %x", expectedRoot, finalRoot)
	}
	return nil
}
