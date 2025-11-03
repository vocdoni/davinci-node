package statetransition

import (
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/signature/eddsa"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/types"
	imt "github.com/vocdoni/lean-imt-go/circuit"
)

// DummySiblings function returns a dummy siblings to fill the vote verifier
// inputs siblings in the VoteVerifierInputs when the census origin is not
// MerkleTree.
func DummySiblings() [types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField] {
	siblings := [types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
	for i := range siblings {
		siblings[i] = emulated.ValueOf[sw_bn254.ScalarField](1)
	}
	return siblings
}

func DummyMerkleProof() imt.MerkleProof {
	// generate dummy siblings for each voter to fill dummy merkle proofs
	dummySiblings := [imt.MaxCensusDepth]frontend.Variable{}
	for i := range dummySiblings {
		dummySiblings[i] = big.NewInt(1) // dummy value for the siblings
	}
	return imt.MerkleProof{
		Leaf:     big.NewInt(1), // dummy value for the key
		Index:    big.NewInt(1), // dummy value for the leaf hash
		Siblings: dummySiblings,
	}
}

// DummyCSPProof function returns a dummy CSP public key and signature to fill
// the vote verifier inputs when the census origin is not CSP.
func DummyCSPProof() csp.CSPProof {
	dummyTwistedPoint := twistededwards.Point{X: 0, Y: 1}
	return csp.CSPProof{
		PublicKey: eddsa.PublicKey{
			A: dummyTwistedPoint,
		},
		Signature: eddsa.Signature{
			R: dummyTwistedPoint,
			S: 1,
		},
	}
}
