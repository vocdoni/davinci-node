package state

import (
	"log"
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo"
)

func TestPlayground(t *testing.T) {
	c := qt.New(t)

	nullifier, ok := new(big.Int).SetString("6177343795568935461175357710059239825061937375900049764701177481196810147448", 10)
	c.Assert(ok, qt.IsTrue)
	safeNullifier := arbo.BytesToBigInt(HashFunc.SafeBigInt(nullifier))
	
	q, _ := new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	
	c.Log("nullifier", nullifier)
	c.Log("        q", q)
	c.Log("     safe", safeNullifier)
	c.Log(" in-field", safeNullifier.Cmp(q) == -1)
	log.Println(arbo.BytesToBigInt(HashFunc.SafeValue(nullifier.Bytes())))
}
