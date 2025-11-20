package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"

	c3api "github.com/vocdoni/census3-bigquery/api"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

func NewCensus3MerkleTreeForTest(ctx context.Context, votes []state.Vote, c3url string) (*big.Int, string, error) {
	// create a new census
	censusId, err := c3NewCensus(c3url)
	if err != nil {
		return nil, "", fmt.Errorf("census3 service error creating census: %w", err)
	}
	log.Printf("created new census in census3 service with ID: %s", censusId)
	// add participants from votes provided
	participants := []c3api.CensusParticipant{}
	for _, v := range votes {
		participants = append(participants, c3api.CensusParticipant{
			Key:    v.Address.Bytes(),
			Weight: new(types.BigInt).SetBigInt(v.Weight),
		})
	}
	if err := c3AddParticipants(c3url, censusId, participants); err != nil {
		return nil, "", fmt.Errorf("census3 service error adding participants: %w", err)
	}
	// get census info: root, size, uri
	root, size, uri, err := c3GetCensusInfo(c3url, censusId)
	if err != nil {
		return nil, "", fmt.Errorf("census3 service error getting info: %w", err)
	}
	// verify size matches participants added
	if size != len(participants) {
		return nil, "", fmt.Errorf("census size mismatch: expected %d, got %d", len(participants), size)
	}
	log.Printf("census %s created.\turi=%s\troot=%s\tsize=%d", censusId, uri, root.String(), size)
	// return the census root and uri
	return root, uri, nil
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
			log.Printf("Warning: failed to close new census response body: %v", err)
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
			log.Printf("Warning: failed to close participants response body: %v", err)
		}
	}()
	if participantsRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(participantsRes.Body, 1024))
		log.Printf("Response body: %s", string(body))
		return fmt.Errorf("unexpected status code adding participants: %d", participantsRes.StatusCode)
	}
	return nil
}

func c3GetCensusInfo(c3url, censusId string) (*big.Int, int, string, error) {
	// get the census size to ensure participants were added making a GET
	// request to /censuses/{censusID}/size
	sizeURL, err := url.JoinPath(c3url, "/censuses/", censusId, "/size")
	if err != nil {
		return nil, 0, "", fmt.Errorf("error creating size URL: %w", err)
	}
	sizeRes, err := http.Get(sizeURL)
	if err != nil {
		return nil, 0, "", fmt.Errorf("error getting census size: %w", err)
	}
	defer func() {
		if err := sizeRes.Body.Close(); err != nil {
			log.Printf("Warning: failed to close size response body: %v", err)
		}
	}()
	if sizeRes.StatusCode != http.StatusOK {
		return nil, 0, "", fmt.Errorf("unexpected status code getting census size: %d", sizeRes.StatusCode)
	}
	var sizeResp c3api.CensusSizeResponse
	if err := json.NewDecoder(sizeRes.Body).Decode(&sizeResp); err != nil {
		return nil, 0, "", fmt.Errorf("error decoding census size response: %w", err)
	}

	// get the census root making a GET request to /censuses/{censusID}/root
	rootURL, err := url.JoinPath(c3url, "/censuses/", censusId, "/root")
	if err != nil {
		return nil, 0, "", fmt.Errorf("error creating root URL: %w", err)
	}
	rootRes, err := http.Get(rootURL)
	if err != nil {
		return nil, 0, "", fmt.Errorf("error getting census root: %w", err)
	}
	defer func() {
		if err := rootRes.Body.Close(); err != nil {
			log.Printf("Warning: failed to close root response body: %v", err)
		}
	}()
	if rootRes.StatusCode != http.StatusOK {
		return nil, 0, "", fmt.Errorf("unexpected status code getting census root: %d", rootRes.StatusCode)
	}
	var rootResp c3api.CensusRootResponse
	if err := json.NewDecoder(rootRes.Body).Decode(&rootResp); err != nil {
		return nil, 0, "", fmt.Errorf("error decoding census root response: %w", err)
	}

	// get the census URI making a GET request to /censuses/{censusID}/uri
	uriURL, err := url.JoinPath(c3url, "/censuses/", censusId, "/uri")
	if err != nil {
		return nil, 0, "", fmt.Errorf("error creating uri URL: %w", err)
	}
	uriRes, err := http.Get(uriURL)
	if err != nil {
		return nil, 0, "", fmt.Errorf("error getting census uri: %w", err)
	}
	defer func() {
		if err := uriRes.Body.Close(); err != nil {
			log.Printf("Warning: failed to close uri response body: %v", err)
		}
	}()
	if uriRes.StatusCode != http.StatusOK {
		return nil, 0, "", fmt.Errorf("unexpected status code getting census uri: %d", uriRes.StatusCode)
	}
	var uriResp c3api.CensusURIResponse
	if err := json.NewDecoder(uriRes.Body).Decode(&uriResp); err != nil {
		return nil, 0, "", fmt.Errorf("error decoding census uri response: %w", err)
	}
	return rootResp.Root.BigInt().MathBigInt(), sizeResp.Size, uriResp.URI, nil
}
