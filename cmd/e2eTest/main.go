package main

import (
	"context"
	"fmt"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/arbo/memdb"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
	"github.com/vocdoni/vocdoni-z-sandbox/web3/rpc/chainlist"
)

const (
	sepoliaProcessRegistry = "0x3d0b39c0239329955b9F0E8791dF9Aa84133c861"
	sepoliaOrgRegistry     = "0xd512481d0Fa6d975f9B186a9f6e59ea8E12D2C2b"

	testLocalAccountPrivKey = "0cebebc37477f513cd8f946ffced46e368aa4f9430250ce4507851edbba86b20" // defined in docker/files/genesis.json
)

func main() {
	privKey := flag.String("privkey", testLocalAccountPrivKey, "private key to use for the Ethereum account")
	sepolia := flag.Bool("sepolia", false, "use sepolia dev deployment")
	w3rpc := flag.String("w3rpc", "http://localhost:8545", "web3 rpc endpoint")

	flag.Parse()
	log.Init("debug", "stdout", nil)

	var err error
	contracts := &web3.Contracts{}

	if *sepolia {
		rpcs, err := chainlist.EndpointList(0, "sep", 10)
		if err != nil {
			log.Fatal(err)
		}
		contracts, err := web3.New(rpcs)
		if err != nil {
			log.Fatal(err)
		}
		if err = contracts.LoadContracts(&web3.Addresses{
			OrganizationRegistry: common.HexToAddress(sepoliaOrgRegistry),
			ProcessRegistry:      common.HexToAddress(sepoliaProcessRegistry),
		}); err != nil {
			log.Fatal(err)
		}

		for i := 1; i < len(rpcs); i++ {
			if err := contracts.AddWeb3Endpoint(rpcs[i]); err != nil {
				log.Warnw("failed to add endpoint", "rpc", rpcs[i], "err", err)
			}
		}

		if err := contracts.SetAccountPrivateKey(*privKey); err != nil {
			log.Fatal(err)
		}

		log.Infow("contracts initialized", "chainId", contracts.ChainID)

	} else {
		contracts, err = web3.DeployContracts(*w3rpc, *privKey)
		if err != nil {
			log.Fatal(err)
		}
		log.Infow("contracts deployed", "chainId", contracts.ChainID)
	}

	// create storage in memory
	stg := storage.New(memdb.New())

	// monitor new processes
	ctx := context.Background()
	pm := service.NewProcessMonitor(contracts, stg, time.Second*2)
	if err := pm.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// start API service
	api := service.NewAPI(stg, "0.0.0.0", 0)
	if err := api.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// monitor new organizations
	newOrgChan, err := contracts.MonitorOrganizationCreatedByPolling(ctx, time.Second*5)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		log.Info("monitoring new organizations")
		for {
			select {
			case <-ctx.Done():
				return
			case org := <-newOrgChan:
				log.Infow("new organization found", "organization", org.String())
			}
		}
	}()

	time.Sleep(20 * time.Second)

	orgAddr := contracts.AccountAddress()
	txHash, err := contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	if err != nil {
		log.Errorw(err, "failed to create organization")
		return
	}

	if err := contracts.WaitTx(txHash, time.Second*30); err != nil {
		log.Errorw(err, "failed to wait for tx")
		return
	}

	curve := curves.New(bjj.CurveType)
	pubKey, _, err := elgamal.GenerateKey(curve)
	if err != nil {
		log.Errorw(err, "failed to generate key")
		return
	}
	x, y := pubKey.Point()

	_, _, err = contracts.CreateProcess(&types.Process{
		Status:         0,
		OrganizationId: contracts.AccountAddress(),
		EncryptionKey: &types.EncryptionKey{
			X: x,
			Y: y,
		},
		StateRoot:   util.RandomBytes(32),
		StartTime:   time.Now().Add(5 * time.Minute),
		Duration:    time.Hour,
		MetadataURI: "https://example.com/metadata",
		BallotMode: &types.BallotMode{
			MaxCount:        2,
			MaxValue:        new(types.BigInt).SetUint64(100),
			MinValue:        new(types.BigInt).SetUint64(0),
			MaxTotalCost:    new(types.BigInt).SetUint64(0),
			MinTotalCost:    new(types.BigInt).SetUint64(0),
			ForceUniqueness: false,
			CostFromWeight:  false,
		},
		Census: &types.Census{
			CensusRoot:   util.RandomBytes(32),
			MaxVotes:     new(types.BigInt).SetUint64(100),
			CensusURI:    "https://example.com/census",
			CensusOrigin: 0,
		},
	})
	if err != nil {
		log.Errorw(err, "failed to create process")
		return
	}
	select {}
}
