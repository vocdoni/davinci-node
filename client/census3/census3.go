package census3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	c3api "github.com/vocdoni/census3-bigquery/api"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/client/httpclient"
	"github.com/vocdoni/davinci-node/types"
)

const (
	CensusIDParamName = "censusID"

	HealthEndpoint          = "/health"
	CreateCensusEndpoint    = "/censuses"
	AddParticipantsEndpoint = "/censuses/{" + CensusIDParamName + "}/participants"
	PublishCensusEndpoint   = "/censuses/{" + CensusIDParamName + "}/publish"
)

// CensusParticipant is a package wrapper for the census3 api CensusParticipant
// struct.
type CensusParticipant c3api.CensusParticipant

// Census3Client struct contains the required information to use a Census3
// service. Includes a context, an internal HTTP client and the enpoint of
// the Census3 service.
type Census3Client struct {
	ctx        context.Context
	httpClient *httpclient.HTTPclient
	endpoint   string
}

// NewClient function creates a new Census3Client based on the provided
// context and service endpoint.
func NewClient(ctx context.Context, endpoint string) (*Census3Client, error) {
	httpClient, err := httpclient.NewHTTPClient(endpoint)
	if err != nil {
		return nil, err
	}
	c3cli := &Census3Client{
		ctx:        ctx,
		httpClient: httpClient,
		endpoint:   endpoint,
	}
	if err := c3cli.ping(); err != nil {
		return nil, err
	}
	return c3cli, nil
}

func (c *Census3Client) ping() error {
	_, status, err := c.httpClient.Request(http.MethodGet, nil, nil, HealthEndpoint)
	if err != nil {
		return fmt.Errorf("error pinging census3 service: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected status code pinging census3 service: %d", status)
	}
	return nil
}

// NewCensus method creates a new census in the service based on the given
// origin and the provided participants. It only allows merkle-tree based
// censuses.
func (c *Census3Client) NewCensus(origin types.CensusOrigin, participants []CensusParticipant) (*types.Census, error) {
	if !origin.IsMerkleTree() {
		return nil, fmt.Errorf("census3 service only supports merkle trees")
	}
	// create a new census
	censusId, err := c.createCensus()
	if err != nil {
		return nil, fmt.Errorf("census3 service error creating census: %w", err)
	}
	// add participants from votes provided
	censusParticipants := []c3api.CensusParticipant{}
	for _, v := range participants {
		censusParticipants = append(censusParticipants, c3api.CensusParticipant{
			Key:    v.Key,
			Weight: v.Weight,
		})
	}
	if err := c.addParticipants(censusId, censusParticipants); err != nil {
		return nil, fmt.Errorf("census3 service error adding participants: %w", err)
	}
	// get census info: root, size, uri
	root, size, uri, err := c.publishCensus(censusId)
	if err != nil {
		return nil, fmt.Errorf("census3 service error getting info: %w", err)
	}
	// verify size matches participants added
	if size != len(participants) {
		return nil, fmt.Errorf("census size mismatch: expected %d, got %d", len(participants), size)
	}
	censusURI, err := url.JoinPath(c.endpoint, uri)
	if err != nil {
		return nil, fmt.Errorf("error creating census URI: %w", err)
	}
	// return the census root and uri
	return &types.Census{
		CensusOrigin: origin,
		CensusRoot:   root,
		CensusURI:    censusURI,
	}, nil
}

func (c *Census3Client) createCensus() (string, error) {
	// create a new census in the census3 service making a POST request to
	// /censuses
	body, status, err := c.httpClient.Request(http.MethodPost, nil, nil, CreateCensusEndpoint)
	if err != nil {
		return "", fmt.Errorf("error creating new census in census3 service: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("unexpected status code creating new census: %d", status)
	}
	var newCensusRes c3api.NewCensusResponse
	if err := json.Unmarshal(body, &newCensusRes); err != nil {
		return "", fmt.Errorf("error decoding new census response: %w", err)
	}
	return newCensusRes.Census, nil
}

func (c *Census3Client) addParticipants(censusID string, participants []c3api.CensusParticipant) error {
	// use the provided votes to add participants to the census making a POST
	// request  to /censuses/{censusID}/participants
	participantsReq := c3api.CensusParticipantsRequest{
		Participants: participants,
	}
	body, status, err := c.httpClient.Request(http.MethodPost, participantsReq, nil, api.EndpointWithParam(AddParticipantsEndpoint, CensusIDParamName, censusID))
	if err != nil {
		return fmt.Errorf("error adding participants to census: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected status code adding participants: %d - %s", status, string(body))
	}
	return nil
}

func (c *Census3Client) publishCensus(censusID string) (types.HexBytes, int, string, error) {
	// publish the census making a POST request to /censuses/{censusID}/publish
	body, status, err := c.httpClient.Request(http.MethodPost, nil, nil, api.EndpointWithParam(PublishCensusEndpoint, CensusIDParamName, censusID))
	if err != nil {
		return nil, 0, "", fmt.Errorf("error getting census size: %w", err)
	}
	var publishRes c3api.PublishCensusResponse
	if err := json.Unmarshal(body, &publishRes); err != nil {
		return nil, 0, "", fmt.Errorf("error decoding publish census response: %w", err)
	}
	if status != http.StatusOK {
		return nil, 0, "", fmt.Errorf("unexpected status code publishing census: %d", status)
	}
	return publishRes.Root, publishRes.Size, publishRes.CensusURI, nil
}
