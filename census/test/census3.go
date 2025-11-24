package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"

	"github.com/ethereum/go-ethereum/common"
	c3api "github.com/vocdoni/census3-bigquery/api"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

func NewCensus3MerkleTreeForTest(ctx context.Context, votes []state.Vote, c3url string) (*big.Int, string, error) {
	// create a new census
	censusId, err := c3NewCensus(c3url)
	if err != nil {
		return nil, "", fmt.Errorf("census3 service error creating census: %w", err)
	}
	log.Infow("new census created in census3 service", "id", censusId)
	// add participants from votes provided
	participants := []c3api.CensusParticipant{}
	for _, v := range votes {
		participants = append(participants, c3api.CensusParticipant{
			Key:    common.BigToAddress(v.Address).Bytes(),
			Weight: new(types.BigInt).SetBigInt(v.Weight),
		})
	}
	if err := c3AddParticipants(c3url, censusId, participants); err != nil {
		return nil, "", fmt.Errorf("census3 service error adding participants: %w", err)
	}
	// get census info: root, size, uri
	root, size, uri, err := c3PublishCensus(c3url, censusId)
	if err != nil {
		return nil, "", fmt.Errorf("census3 service error getting info: %w", err)
	}
	// verify size matches participants added
	if size != len(participants) {
		return nil, "", fmt.Errorf("census size mismatch: expected %d, got %d", len(participants), size)
	}
	censusURI, err := url.JoinPath(c3url, uri)
	if err != nil {
		return nil, "", fmt.Errorf("error creating census URI: %w", err)
	}
	log.Infow("census published in census3 service",
		"id", censusId,
		"root", root.String(),
		"size", size,
		"uri", censusURI,
	)
	// return the census root and uri
	return root, censusURI, nil
}

func c3NewCensus(c3url string) (string, error) {
	// create a new census in the census3 service making a POST request to
	// /censuses
	newCensusURL, err := url.JoinPath(c3url, "/censuses")
	if err != nil {
		return "", fmt.Errorf("error creating new census URL: %w", err)
	}
	newCensusRes, err := http.Post(newCensusURL, "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("error creating new census in census3 service: %w", err)
	}
	defer func() {
		if err := newCensusRes.Body.Close(); err != nil {
			log.Errorw(err, "error closing new census response body")
		}
	}()
	if newCensusRes.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code creating new census: %d", newCensusRes.StatusCode)
	}
	var newCensusResp c3api.NewCensusResponse
	if err := json.NewDecoder(newCensusRes.Body).Decode(&newCensusResp); err != nil {
		return "", fmt.Errorf("error decoding new census response: %w", err)
	}
	return newCensusResp.Census, nil
}

func c3AddParticipants(c3url, censusID string, participants []c3api.CensusParticipant) error {
	// use the provided votes to add participants to the census making a POST
	// request  to /censuses/{censusID}/participants
	participantsURL, err := url.JoinPath(c3url, "/censuses/", censusID, "/participants")
	if err != nil {
		return fmt.Errorf("error creating participants URL: %w", err)
	}
	participantsBody, err := json.Marshal(c3api.CensusParticipantsRequest{
		Participants: participants,
	})
	if err != nil {
		return fmt.Errorf("error marshaling participants request: %w", err)
	}
	participantsRes, err := http.Post(participantsURL, "application/json", bytes.NewReader(participantsBody))
	if err != nil {
		return fmt.Errorf("error adding participants to census: %w", err)
	}
	defer func() {
		if err := participantsRes.Body.Close(); err != nil {
			log.Errorw(err, "error closing participants response body")
		}
	}()
	if participantsRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(participantsRes.Body, 1024))
		return fmt.Errorf("unexpected status code adding participants: %d - %s", participantsRes.StatusCode, string(body))
	}
	return nil
}

func c3PublishCensus(c3url, censusId string) (*big.Int, int, string, error) {
	// publish the census making a POST request to /censuses/{censusID}/publish
	publishURL, err := url.JoinPath(c3url, "/censuses/", censusId, "/publish")
	if err != nil {
		return nil, 0, "", fmt.Errorf("error creating size URL: %w", err)
	}
	res, err := http.Post(publishURL, "plain/text", nil)
	if err != nil {
		return nil, 0, "", fmt.Errorf("error getting census size: %w", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Errorw(err, "error closing publish census response body")
		}
	}()
	var publishRes c3api.PublishCensusResponse
	if err := json.NewDecoder(res.Body).Decode(&publishRes); err != nil {
		return nil, 0, "", fmt.Errorf("error decoding publish census response: %w", err)
	}
	return publishRes.Root.BigInt().MathBigInt(), publishRes.Size, publishRes.CensusURI, nil
}
