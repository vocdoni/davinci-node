package dkg

import (
	"crypto/rand"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_iden3"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
)

func TestDKG(t *testing.T) {
	const (
		maxValue    = 5   // Number of candidates (e.g., 0 to 4)
		numVoters   = 100 // Number of voters
		threshold   = 3   // Threshold for DKG
		maxParallel = 100 // Maximum parallel vote processing
	)
	c := qt.New(t)

	// Initialize curve
	curvePoint := curves.New(bjj.CurveType)

	// Initialize participants
	participantIDs := []int{1, 2, 3, 4, 5}
	participants := make(map[int]*Participant)
	for _, id := range participantIDs {
		participants[id] = NewParticipant(id, threshold, participantIDs, curvePoint)
		participants[id].GenerateSecretPolynomial()
	}

	// Exchange commitments and shares
	allPublicCoeffs := make(map[int][]ecc.Point)
	for id, p := range participants {
		allPublicCoeffs[id] = p.PublicCoeffs
	}

	// Each participant computes shares to send
	for _, p := range participants {
		p.ComputeShares()
	}

	// Participants exchange shares and verify
	for _, p := range participants {
		for id, otherP := range participants {
			if p.ID != id {
				share := otherP.SecretShares[p.ID]
				err := p.ReceiveShare(id, share, otherP.PublicCoeffs)
				c.Assert(err, qt.IsNil, qt.Commentf("Participant %d failed to verify share from %d", p.ID, id))
			}
		}
	}

	// Each participant aggregates shares
	for _, p := range participants {
		p.AggregateShares()
	}

	// Compute aggregated public key
	for _, p := range participants {
		p.AggregatePublicKey(allPublicCoeffs)
	}

	// Verify all participants computed the same public key
	firstPubKey := participants[1].PublicKey
	for _, p := range participants {
		c.Assert(p.PublicKey.Equal(firstPubKey), qt.IsTrue, qt.Commentf("Public key mismatch for participant %d", p.ID))
	}

	// Test voting simulation
	expectedSum := big.NewInt(0)
	maxMessage := uint64(maxValue*numVoters) + 1

	// Initialize aggregate ciphertexts
	aggC1 := curvePoint.New()
	aggC1.SetZero()
	aggC2 := curvePoint.New()
	aggC2.SetZero()

	// Simulate voting
	var votesDone atomic.Uint32
	wg := sync.WaitGroup{}
	sem := make(chan struct{}, maxParallel)

	for i := 0; i < numVoters; i++ {
		voteValue, err := rand.Int(rand.Reader, big.NewInt(int64(maxValue)))
		c.Assert(err, qt.IsNil)
		expectedSum.Add(expectedSum, voteValue)

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() {
				wg.Done()
				votesDone.Add(1)
				<-sem
			}()

			c1, c2, _, err := elgamal.Encrypt(participants[1].PublicKey, voteValue)
			c.Assert(err, qt.IsNil)

			// Aggregate ciphertexts
			aggC1.SafeAdd(aggC1, c1)
			aggC2.SafeAdd(aggC2, c2)
		}()
	}
	wg.Wait()

	// Test decryption
	partialDecryptions := make(map[int]ecc.Point)
	participantSubset := []int{1, 2, 3} // Using threshold number of participants
	for _, id := range participantSubset {
		p := participants[id]
		pd := p.ComputePartialDecryption(aggC1)
		partialDecryptions[id] = pd
	}

	// Combine partial decryptions to recover the sum of votes
	decryptedSum, err := CombinePartialDecryptions(aggC2, partialDecryptions, participantSubset, maxMessage)
	c.Assert(err, qt.IsNil)

	// Verify the sum
	c.Assert(decryptedSum.Cmp(expectedSum), qt.Equals, 0, qt.Commentf("Decrypted sum does not match expected sum"))

	c.Run("distributed-proof", func(c *qt.C) {
		// 1. each trustee builds and shares commitments
		commitments := make(map[int]CPCommitment)
		for _, id := range participantSubset {
			com, err := participants[id].BuildCommitment(aggC1)
			c.Assert(err, qt.IsNil)
			commitments[id] = com
		}

		// everyone receives everyone else's commitments (simulate network)
		for _, p := range participants {
			for oid, com := range commitments {
				p.ReceiveCommitment(oid, com)
			}
		}

		// sum commitments
		sumA1 := curvePoint.New()
		sumA1.SetZero()
		sumA2 := curvePoint.New()
		sumA2.SetZero()
		for _, com := range commitments {
			sumA1.Add(sumA1, com.A1)
			sumA2.Add(sumA2, com.A2)
		}

		// Lagrange coefficients for subset
		lambda, err := computeLagrangeCoefficients(participantSubset, curvePoint.Order())
		c.Assert(err, qt.IsNil)

		// aggregate share S = Σ λᵢ·Sᵢ
		S := curvePoint.New()
		S.SetZero()
		for _, id := range participantSubset {
			term := S.New()
			term.ScalarMult(partialDecryptions[id], lambda[id])
			S.Add(S, term)
		}

		// compute Fiat–Shamir challenge e = H(G,P,C1,S,A1,A2)
		// use helper from elgamal package
		e := elgamal.HashPointsToScalar(
			firstPubKey, // G implicit -> use P for domain‑sep as in verifier
			firstPubKey,
			aggC1,
			S,
			sumA1,
			sumA2,
		)

		// 2. trustees build partial responses z_i and share
		partZ := make(map[int]*big.Int)
		for _, id := range participantSubset {
			zi := participants[id].BuildPartialResponse(e, lambda[id], curvePoint.Order())
			partZ[id] = zi
		}

		// everyone receives all z (simulate)
		for _, p := range participants {
			for oid, z := range partZ {
				p.ReceivePartialResponse(oid, z)
			}
		}

		// 3. combiner assembles the proof
		proof, err := AssembleDecryptionProof(firstPubKey, aggC1, aggC2, decryptedSum, commitments, partZ)
		c.Assert(err, qt.IsNil)

		// 4. verify proof with canonical verifier
		c.Assert(elgamal.VerifyDecryptionProof(firstPubKey, aggC1, aggC2, decryptedSum, proof), qt.IsNil)
	})

}
