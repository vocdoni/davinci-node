package census

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/census/censusdb"
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
	Insecure:     false,
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
func (e graphqlResponseData) toGraphQLEvent() (census.CensusEvent, error) {
	// Parse account ID to Ethereum address
	addr := common.HexToAddress(e.Account.ID)
	if addr == (common.Address{}) {
		return census.CensusEvent{}, fmt.Errorf("invalid account address: %s", e.Account.ID)
	}
	// Parse previous and new weights to big.Int
	prevWeight, ok := new(big.Int).SetString(e.PreviousWeight, 10)
	if !ok {
		return census.CensusEvent{}, fmt.Errorf("invalid previous weight: %s", e.PreviousWeight)
	}
	newWeight, ok := new(big.Int).SetString(e.NewWeight, 10)
	if !ok {
		return census.CensusEvent{}, fmt.Errorf("invalid new weight: %s", e.NewWeight)
	}
	// Return the constructed graphqlEvent
	return census.CensusEvent{
		Address:    addr,
		PrevWeight: prevWeight,
		NewWeight:  newWeight,
	}, nil
}

// GraphQLImporterConfig holds configuration options for the GraphQL importer.
type GraphQLImporterConfig struct {
	PageSize     int
	QueryTimeout time.Duration
	Insecure     bool
}

// GraphQLImporter returns an instance of graphqlImporter with the provided
// configuration. If config is nil, it uses the default configuration.
func GraphQLImporter(config *GraphQLImporterConfig) *graphqlImporter {
	if config == nil {
		config = &defaultConfig
	}
	if config.PageSize <= 0 {
		config.PageSize = defaultConfig.PageSize
	}
	if config.QueryTimeout <= 0 {
		config.QueryTimeout = defaultConfig.QueryTimeout
	}
	return &graphqlImporter{
		queryTimeout: config.QueryTimeout,
		pageSize:     config.PageSize,
		insecure:     config.Insecure,
	}
}

// graphqlImporter is an implementation of the ImporterPlugin interface for
// importing censuses from GraphQL endpoints.
type graphqlImporter struct {
	queryTimeout time.Duration
	pageSize     int
	insecure     bool
}

// ValidURI checks if the provided targetURI is a valid GraphQL endpoint URL
// (starts with "graphql://").
func (d *graphqlImporter) ValidURI(targetURI string) bool {
	return strings.HasPrefix(targetURI, "graphql://")
}

// ImportCensus downloads the census merkle tree events from the
// specified targetURI and imports them into the census DB based on the
// expectedRoot. It returns an error if the download or import fails.
func (d *graphqlImporter) ImportCensus(
	ctx context.Context,
	censusDB *censusdb.CensusDB,
	census *types.Census,
	processedElements int,
) (int, error) {
	// Parse the GraphQL endpoint from the target URI
	endpoint, err := endpointFromURI(census.CensusURI, d.insecure)
	if err != nil {
		return 0, fmt.Errorf("invalid GraphQL URI: %w", err)
	}
	// Get the graphql events from the target URI
	events, err := queryEvents(ctx, endpoint, processedElements, d.pageSize, d.queryTimeout, d.insecure)
	if err != nil {
		return 0, fmt.Errorf("failed to query GraphQL events from %s: %w", census.CensusURI, err)
	}
	// Do not return error if no events are found
	if len(events) == 0 {
		return processedElements, nil
	}
	// If the census does not exists, import all the received events as new census, otherwise
	// update the existing census with the new events
	if !censusDB.ExistsByAddress(census.ContractAddress) {
		// Import all the available events into the census DB
		if _, err := censusDB.ImportEventsByAddress(census.ContractAddress, census.CensusRoot, events); err != nil {
			return 0, fmt.Errorf("failed to import census from events: %w", err)
		}
	} else {
		// Get the reference of the census by its old root
		ref, err := censusDB.LoadByAddress(census.ContractAddress)
		if err != nil {
			return 0, fmt.Errorf("failed to load census by address %s: %w", census.ContractAddress.Hex(), err)
		}
		// Update the census with the new events
		if err = ref.ApplyEvents(events); err != nil {
			return 0, fmt.Errorf("failed to update census from events: %w", err)
		}
	}
	return processedElements + len(events), nil
}

// endpointFromURI converts a GraphQL URI (starting with "graphql://") to an
// HTTP endpoint by replacing the scheme with "http://".
func endpointFromURI(targetURI string, insecure bool) (string, error) {
	if !strings.HasPrefix(targetURI, "graphql://") {
		return "", fmt.Errorf("invalid GraphQL URI: %s", targetURI)
	}
	protocol := "https://"
	if insecure {
		protocol = "http://"
	}
	return protocol + strings.TrimPrefix(targetURI, "graphql://"), nil
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

// queryEvents fetches weight change events from the specified GraphQL
// endpoint using pagination. It returns a slice of graphqlEvent and the
// last item queryed or an error if the query fails. It iterates through
// pages until no more events are available, parsing the responses and
// accumulating the results.
func queryEvents(
	ctx context.Context,
	url string,
	from int,
	pageSize int,
	timeout time.Duration,
	insecure bool,
) ([]census.CensusEvent, error) {
	// Setup pagination variables
	skip := from
	first := pageSize
	// Create the result slice and http client to be reused
	var results []census.CensusEvent
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
		},
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
				// Read the response body
				body, err := io.ReadAll(res.Body)
				if err != nil {
					return fmt.Errorf("error reading response body: %v", err)
				}
				// Check for non-200 status codes
				if res.StatusCode != http.StatusOK {
					return fmt.Errorf("non-200 response: %d - %s", res.StatusCode, string(body))
				}
				// Decode the GraphQL response
				var gqlResp graphqlResponse
				if err := json.Unmarshal(body, &gqlResp); err != nil {
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
			// last page, if so, return the results and the last index queryed
			if receivedEvents < pageSize {
				return results, nil
			}
			// Update skip for next page
			skip += pageSize
		}
	}
}
