package metadata

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	cid "github.com/ipfs/go-cid"
	"github.com/vocdoni/davinci-node/types"
)

func CID(v any) (types.HexBytes, []byte, error) {
	// Encode the value as JSON.
	data, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal json: %w", err)
	}

	sum := sha256.Sum256(data)
	// Manual multihash for sha2-256:
	// 0x12 = sha2-256 code
	// 0x20 = 32-byte digest length
	multihash := make([]byte, 0, 2+len(sum))
	multihash = append(multihash, 0x12, 0x20)
	multihash = append(multihash, sum[:]...)

	// CIDv1 + raw codec.
	c := cid.NewCidV1(cid.Raw, multihash)
	return c.Bytes(), data, nil
}
