package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"

	blockservice "github.com/ipfs/boxo/blockservice"
	blockstore "github.com/ipfs/boxo/blockstore"
	chunk "github.com/ipfs/boxo/chunker"
	offline "github.com/ipfs/boxo/exchange/offline"
	merkledag "github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	ihelper "github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	cid "github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	mh "github.com/multiformats/go-multihash"
	"github.com/vocdoni/davinci-node/types"
)

const chunkerTypeSize = "size-262144"

// HexBytesToCID converts a types.HexBytes to a cid.Cid
func HexBytesToCID(b types.HexBytes) (cid.Cid, error) {
	return cid.Cast([]byte(b))
}

// CIDToHexBytes converts a cid.Cid to a types.HexBytes
func CIDToHexBytes(c cid.Cid) types.HexBytes {
	return types.HexBytes(c.Bytes())
}

// CIDStringToHexBytes converts a string to a types.HexBytes cid
func CIDStringToHexBytes(s string) types.HexBytes {
	c, err := cid.Parse(s)
	if err != nil {
		return nil
	}
	return CIDToHexBytes(c)
}

// CID calculates the IPFS Cid hash (v1) from a bytes buffer, using parameters:
//   - Codec: DagProtobuf
//   - MhType: SHA2_256
func CID(v any) (types.HexBytes, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}

	bstore := blockstore.NewBlockstore(
		dssync.MutexWrap(ds.NewMapDatastore()),
		blockstore.NoPrefix(),
	)
	bsrv := blockservice.New(bstore, offline.Exchange(bstore))
	dagServ := merkledag.NewDAGService(bsrv)

	chnk, err := chunk.FromString(bytes.NewReader(data), chunkerTypeSize)
	if err != nil {
		return nil, fmt.Errorf("create chunker: %w", err)
	}

	params := ihelper.DagBuilderParams{
		Dagserv:   dagServ,
		Maxlinks:  ihelper.DefaultLinksPerBlock,
		RawLeaves: true,
		CidBuilder: cid.V1Builder{
			Codec:    cid.DagProtobuf,
			MhType:   mh.SHA2_256,
			MhLength: -1,
		},
	}

	dbh, err := params.New(chnk)
	if err != nil {
		return nil, fmt.Errorf("create dag builder: %w", err)
	}

	nd, err := balanced.Layout(dbh)
	if err != nil {
		return nil, fmt.Errorf("build balanced layout: %w", err)
	}

	return CIDToHexBytes(nd.Cid()), nil
}
