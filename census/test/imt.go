package test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/util"
	imt "github.com/vocdoni/lean-imt-go"
	imtcensus "github.com/vocdoni/lean-imt-go/census"
)

func CensusIMTForTest(votes []state.Vote) (*imtcensus.CensusIMT, error) {
	// generate the census with voters information
	votersData := map[*big.Int]*big.Int{}
	for _, v := range votes {
		votersData[v.Address] = v.Weight
	}

	// Create a unique directory name to avoid lock conflicts
	// Include timestamp and process info for uniqueness
	timestamp := time.Now().UnixNano()
	censusDir := fmt.Sprintf("../assets/census_%d", timestamp)

	// Ensure the assets directory exists
	if err := os.MkdirAll("../assets", 0o755); err != nil {
		return nil, fmt.Errorf("failed to create assets directory: %w", err)
	}

	// Initialize the census merkle tree
	censusTree, err := imtcensus.NewCensusIMTWithPebble(censusDir, imt.PoseidonHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create census IMT: %w", err)
	}

	// Clean up the census directory when done
	defer func() {
		if err := censusTree.Close(); err != nil {
			log.Printf("Warning: failed to close census IMT: %v", err)
		}
		if err := os.RemoveAll(censusDir); err != nil {
			log.Printf("Warning: failed to cleanup census directory %s: %v", censusDir, err)
		}
	}()

	bAddresses, bWeights := []common.Address{}, []*big.Int{}
	for address, weight := range votersData {
		bAddresses = append(bAddresses, common.BigToAddress(address))
		bWeights = append(bWeights, weight)
	}
	if err := censusTree.AddBulk(bAddresses, bWeights); err != nil {
		return nil, fmt.Errorf("failed to add bulk to census IMT: %w", err)
	}
	return censusTree, nil
}

// ServeCensusIMTForTest starts an HTTP server to serve the census
// merkle tree dump for testing purposes. It returns the URL where the census
// can be accessed. The server will run until the provided context is canceled.
func ServeCensusIMTForTest(ctx context.Context, votes []state.Vote) (*big.Int, string, error) {
	// create the census merkle tree
	census, err := CensusIMTForTest(votes)
	if err != nil {
		return nil, "", fmt.Errorf("error generating census merkle tree: %w", err)
	}
	// dump the census merkle tree to assets for external use
	dump, err := census.DumpAll()
	if err != nil {
		return nil, "", fmt.Errorf("error dumping census merkle tree: %w", err)
	}
	// convert to json
	dumpJSON, err := json.Marshal(dump)
	if err != nil {
		return nil, "", fmt.Errorf("error marshaling census dump to json: %w", err)
	}

	// create a handler to serve the census dump
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(dumpJSON); err != nil {
			log.Printf("Warning: failed to write census dump response: %v", err)
		}
	})

	// get an available port
	port := util.RandomInt(4000, 6000)
	addr := fmt.Sprintf("localhost:%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// start the server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Warning: failed to serve census merkle tree: %v", err)
		}
	}()

	// shutdown server on context cancel
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Warning: error shutting down server: %v", err)
		}
	}()

	dumpURL := fmt.Sprintf("http://%s", addr)
	return dump.Root, dumpURL, nil
}
