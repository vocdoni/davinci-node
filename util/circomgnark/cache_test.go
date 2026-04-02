package circomgnark

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
)

func TestUnmarshalCircomVerificationKeyJSONCachesByInput(t *testing.T) {
	c := qt.New(t)

	first, err := UnmarshalCircomVerificationKeyJSON(ballotproof.CircomVerificationKey)
	c.Assert(err, qt.IsNil)

	second, err := UnmarshalCircomVerificationKeyJSON(ballotproof.CircomVerificationKey)
	c.Assert(err, qt.IsNil)

	c.Assert(first == second, qt.IsTrue)
}

func TestCircomVerificationKeyToGnarkCachesResult(t *testing.T) {
	c := qt.New(t)

	circomVK, err := UnmarshalCircomVerificationKeyJSON(ballotproof.CircomVerificationKey)
	c.Assert(err, qt.IsNil)

	first, err := circomVK.ToGnark()
	c.Assert(err, qt.IsNil)

	second, err := circomVK.ToGnark()
	c.Assert(err, qt.IsNil)

	c.Assert(first == second, qt.IsTrue)
}

func TestToGnarkRecursionFixedVkSkipsVerificationKeyConversion(t *testing.T) {
	c := qt.New(t)

	proof := &CircomProof{
		PiA: []string{
			"11711080065308007682838320732817046446099838935738802330966049640254691191206",
			"17850983632338003012778437738834870617367310716158639359113239661166347758019",
			"1",
		},
		PiB: [][]string{
			{
				"8825235276620994813470418380223243496774006915527119606983048301671588119024",
				"2226303267471545357145519696076194402397717063319440340904386422082448596035",
			},
			{
				"8568618036867104573055602703586133087480374891027401247685709167152098077693",
				"1803786236097892649915632611783799060891816616100592289329778375159898063175",
			},
			{"1", "0"},
		},
		PiC: []string{
			"443251187306603655641512095920183574737557831206616603914644748264416016054",
			"4110411832118690910191887320272248494012149664813960539989768130756673868858",
			"1",
		},
		Protocol: "groth16",
	}

	recursionProof, err := proof.ToGnarkRecursion(&CircomVerificationKey{}, []string{
		"1220176476709744867553669165001455267652926745576",
		"1150356464581538673947970931497476016316344861907",
		"16723164540749581091497422535343559644999010121456604327047742204957502303153",
	}, true)
	c.Assert(err, qt.IsNil)
	c.Assert(recursionProof, qt.Not(qt.IsNil))
}
