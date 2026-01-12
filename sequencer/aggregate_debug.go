package sequencer

import (
	"fmt"
	"math/big"
	"reflect"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

func (s *Sequencer) debugAggregationFailure(
	processID types.ProcessID,
	assignment *aggregator.AggregatorCircuit,
	batchInputs *aggregator.AggregatorInputs,
	batchInputsHash *big.Int,
	proveErr error,
) {
	if log.Level() != log.LogLevelDebug {
		return
	}

	log.Warnw("aggregator proving failed; investigating batch inputs",
		"processID", processID.String(),
		"error", proveErr.Error(),
		"validProofs", len(batchInputs.VerifiedBallots),
		"inputsHash", batchInputsHash.String(),
		"voteVerifierNbPublicWitness", s.vvVk.NbPublicWitness(),
		"voteVerifierNbConstraints", s.vvCcs.GetNbConstraints(),
		"aggregatorNbConstraints", s.aggCcs.GetNbConstraints(),
	)

	if pubW, err := frontend.NewWitness(assignment, params.AggregatorCurve.ScalarField(), frontend.PublicOnly()); err != nil {
		log.Warnw("failed to build aggregator public witness", "processID", processID.String(), "error", err.Error())
	} else {
		log.Debugw("aggregator public witness",
			"processID", processID.String(),
			"vector", witnessVectorStrings(pubW),
		)
	}

	proofInputsHashStrings := bigIntStrings(batchInputs.ProofsInputsHashInputs)
	hashPrefix, hashSuffix := prefixSuffixStrings(proofInputsHashStrings, 5)
	log.Debugw("aggregator inputs hash preimage (vote verifier inputs hashes)",
		"processID", processID.String(),
		"count", len(proofInputsHashStrings),
		"prefix", hashPrefix,
		"suffix", hashSuffix,
	)

	opts := stdgroth16.GetNativeVerifierOptions(
		params.AggregatorCurve.ScalarField(),
		params.VoteVerifierCurve.ScalarField(),
	)

	for i, vb := range batchInputs.VerifiedBallots {
		if vb == nil {
			log.Warnw("nil verified ballot in aggregation batch",
				"processID", processID.String(),
				"index", i,
			)
			continue
		}
		if vb.Proof == nil {
			log.Warnw("missing vote verifier proof in aggregation batch",
				"processID", processID.String(),
				"index", i,
				"voteID", vb.VoteID.String(),
				"address", vb.Address.String(),
			)
			continue
		}
		if vb.InputsHash == nil {
			log.Warnw("missing vote verifier inputs hash in aggregation batch",
				"processID", processID.String(),
				"index", i,
				"voteID", vb.VoteID.String(),
				"address", vb.Address.String(),
			)
			continue
		}

		inputsHashValue := emulated.ValueOf[sw_bn254.ScalarField](vb.InputsHash)
		pubAssignment := &voteverifier.VerifyVoteCircuit{
			IsValid:    1,
			InputsHash: inputsHashValue,
		}
		pubWitness, err := frontend.NewWitness(pubAssignment, params.VoteVerifierCurve.ScalarField(), frontend.PublicOnly())
		if err != nil {
			log.Warnw("failed to build vote verifier public witness",
				"processID", processID.String(),
				"index", i,
				"voteID", vb.VoteID.String(),
				"address", vb.Address.String(),
				"inputsHash", vb.InputsHash.String(),
				"error", err.Error(),
			)
			continue
		}

		if err := groth16.Verify(vb.Proof, s.vvVk, pubWitness, opts); err != nil {

			pubAssignmentIsValid0 := &voteverifier.VerifyVoteCircuit{
				IsValid:    0,
				InputsHash: inputsHashValue,
			}
			pubWitnessIsValid0, errIsValid0 := frontend.NewWitness(pubAssignmentIsValid0, params.VoteVerifierCurve.ScalarField(), frontend.PublicOnly())
			if errIsValid0 == nil {
				if err2 := groth16.Verify(vb.Proof, s.vvVk, pubWitnessIsValid0, opts); err2 == nil {
					log.Warnw("vote verifier proof verifies only with IsValid=0; aggregator treating it as real will fail",
						"processID", processID.String(),
						"index", i,
						"voteID", vb.VoteID.String(),
						"address", vb.Address.String(),
						"inputsHash", vb.InputsHash.String(),
						"publicWitnessIsValid0", witnessVectorStrings(pubWitnessIsValid0),
					)
					continue
				}
			}

			log.Warnw("vote verifier proof does not verify (native)",
				"processID", processID.String(),
				"index", i,
				"voteID", vb.VoteID.String(),
				"address", vb.Address.String(),
				"inputsHash", vb.InputsHash.String(),
				"publicWitness", witnessVectorStrings(pubWitness),
				"error", err.Error(),
			)
			continue
		}

		log.Debugw("vote verifier proof verifies (native)",
			"processID", processID.String(),
			"index", i,
			"voteID", vb.VoteID.String(),
			"address", vb.Address.String(),
			"inputsHash", vb.InputsHash.String(),
			"publicWitness", witnessVectorStrings(pubWitness),
		)
	}
}

func witnessVectorStrings(w witness.Witness) []string {
	vecAny := w.Vector()
	rv := reflect.ValueOf(vecAny)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	out := make([]string, rv.Len())
	for i := range rv.Len() {
		out[i] = fmt.Sprint(rv.Index(i).Interface())
	}
	return out
}

func bigIntStrings(v []*big.Int) []string {
	out := make([]string, 0, len(v))
	for _, n := range v {
		if n == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, n.String())
	}
	return out
}

func prefixSuffixStrings(v []string, maxEach int) (prefix, suffix []string) {
	if maxEach <= 0 || len(v) == 0 {
		return nil, nil
	}
	if len(v) <= maxEach*2 {
		return v, nil
	}
	prefix = v[:maxEach]
	suffix = v[len(v)-maxEach:]
	return prefix, suffix
}
