package web3

import "github.com/vocdoni/davinci-node/types"

func bytes32FromCensusRoot(root types.HexBytes) [32]byte {
	normalized := types.NormalizedCensusRoot(root)
	var out [32]byte
	copy(out[:], normalized)
	return out
}
