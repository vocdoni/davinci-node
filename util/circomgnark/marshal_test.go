package circomgnark

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestMarshalCircomProofJSON(t *testing.T) {
	c := qt.New(t)

	proof := &CircomProof{
		PiA:      []string{"1", "2", "1"},
		PiB:      [][]string{{"1", "2"}, {"3", "4"}, {"1", "0"}},
		PiC:      []string{"5", "6", "1"},
		Protocol: "groth16",
	}

	data, err := MarshalCircomProofJSON(proof)
	c.Assert(err, qt.IsNil)

	decoded, err := UnmarshalCircomProofJSON(data)
	c.Assert(err, qt.IsNil)
	c.Assert(decoded, qt.DeepEquals, proof)
}

func TestMarshalCircomVerificationKeyJSON(t *testing.T) {
	c := qt.New(t)

	vk := &CircomVerificationKey{
		Protocol:      "groth16",
		Curve:         "bn128",
		NPublic:       3,
		VkAlpha1:      []string{"1", "2", "1"},
		VkBeta2:       [][]string{{"1", "2"}, {"3", "4"}, {"1", "0"}},
		VkGamma2:      [][]string{{"5", "6"}, {"7", "8"}, {"1", "0"}},
		VkDelta2:      [][]string{{"9", "10"}, {"11", "12"}, {"1", "0"}},
		IC:            [][]string{{"13", "14", "1"}},
		VkAlphabeta12: [][][]string{{{"15", "16"}, {"17", "18"}}},
	}

	data, err := MarshalCircomVerificationKeyJSON(vk)
	c.Assert(err, qt.IsNil)

	decoded, err := UnmarshalCircomVerificationKeyJSON(data)
	c.Assert(err, qt.IsNil)
	c.Assert(decoded, qt.DeepEquals, vk)
}

func TestMarshalCircomPublicSignalsJSON(t *testing.T) {
	c := qt.New(t)

	publicSignals := []string{"1", "2", "3"}

	data, err := MarshalCircomPublicSignalsJSON(publicSignals)
	c.Assert(err, qt.IsNil)

	decoded, err := UnmarshalCircomPublicSignalsJSON(data)
	c.Assert(err, qt.IsNil)
	c.Assert(decoded, qt.DeepEquals, publicSignals)
}
