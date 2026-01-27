package censusdb

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/google/uuid"
)

// TestSolidityCompatibility verifies that our censusdb implementation produces
// the same leaf values and Merkle root as the Solidity DavinciDaoCensus contract.
func TestSolidityCompatibility(t *testing.T) {
	c := qt.New(t)
	c.Parallel()

	// Test data from Solidity contract
	type testNode struct {
		index   int
		address string
		weight  uint64
		leaf    string // expected leaf value as decimal string
	}

	nodes := []testNode{
		{
			index:   0,
			address: "0x11311A2D24a77b6722D7F149B1D9C07C9Bdea16c",
			weight:  3,
			leaf:    "30375291384970416511893979679789548485304528155904142667949947072733511683",
		},
		{
			index:   1,
			address: "0xdeb8699659bE5d41a0e57E179d6cB42E00B9200C",
			weight:  5,
			leaf:    "393512816336772966013610099784681212633281617183806452230580222634896654341",
		},
		{
			index:   2,
			address: "0xB1F05B11Ba3d892EdD00f2e7689779E2B8841827",
			weight:  10,
			leaf:    "314390804811074276967079782683711089676526237735633884656712510764325273610",
		},
		{
			index:   3,
			address: "0xf3B06b503652a5E075D423F97056DFde0C4b066F",
			weight:  1,
			leaf:    "430561437259806371587364395789749002591099599069915338412709746798562902017",
		},
		{
			index:   4,
			address: "0x74D8967e812de34702eCD3D453a44bf37440b10b",
			weight:  3,
			leaf:    "206449094039689427672812727578991218956029384713924405301323341242967261187",
		},
	}

	expectedRoot := "2787380653956260171806300121381944173535678873703019698747166416543300224801"

	// Create a new census
	censusDB := NewCensusDB(newDatabase(t))
	ref, err := censusDB.New(uuid.New())
	c.Assert(err, qt.IsNil)

	// Insert nodes in order and verify leaf values
	for _, node := range nodes {
		// Convert address to 20-byte array
		addr := common.HexToAddress(node.address)

		// Convert weight to bytes (8 bytes, big-endian)
		weightBytes := make([]byte, 8)
		weightBig := new(big.Int).SetUint64(node.weight)
		weightBig.FillBytes(weightBytes)

		// Insert into census
		err := ref.Insert(addr.Bytes(), weightBytes)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to insert node %d", node.index))

		// Calculate expected packed leaf: (address << 88) | weight
		expectedLeaf := new(big.Int)
		expectedLeaf.SetString(node.leaf, 10)

		// Calculate actual packed leaf
		addrBig := new(big.Int).SetBytes(addr.Bytes())
		actualLeaf := new(big.Int).Lsh(addrBig, 88)
		actualLeaf.Or(actualLeaf, weightBig)

		// Verify the packed leaf matches expected
		c.Assert(actualLeaf.String(), qt.Equals, expectedLeaf.String(),
			qt.Commentf("Leaf mismatch for node %d (address: %s, weight: %d)",
				node.index, node.address, node.weight))

		t.Logf("Node %d: address=%s, weight=%d, leaf=%s ✓",
			node.index, node.address, node.weight, actualLeaf.String())
	}

	// Verify the final Merkle root
	actualRoot := ref.Root()
	c.Assert(actualRoot, qt.IsNotNil, qt.Commentf("Root should not be nil"))

	actualRootBig := new(big.Int).SetBytes(actualRoot)
	expectedRootBig := new(big.Int)
	expectedRootBig.SetString(expectedRoot, 10)

	c.Assert(actualRootBig.String(), qt.Equals, expectedRootBig.String(),
		qt.Commentf("Merkle root mismatch. Expected: %s, Got: %s",
			expectedRoot, actualRootBig.String()))

	t.Logf("Final Merkle root: %s ✓", actualRootBig.String())

	// Verify tree size
	size := ref.Size()
	c.Assert(size, qt.Equals, len(nodes),
		qt.Commentf("Tree size mismatch. Expected: %d, Got: %d", len(nodes), size))

	// Generate and verify proofs for each address
	for _, node := range nodes {
		addr := common.HexToAddress(node.address)

		// Generate proof
		proof, err := ref.GenProof(addr.Bytes())
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate proof for node %d", node.index))
		c.Assert(proof.Address.Bytes(), qt.DeepEquals, addr.Bytes(), qt.Commentf("Key mismatch for node %d", node.index))

		// Verify the proof value matches expected leaf
		valueBig := proof.Value.BigInt().MathBigInt()
		expectedLeafBig := new(big.Int)
		expectedLeafBig.SetString(node.leaf, 10)

		c.Assert(valueBig.String(), qt.Equals, expectedLeafBig.String(),
			qt.Commentf("Proof value mismatch for node %d", node.index))

		// Verify the proof is valid
		isValid := VerifyProof(proof.Address, proof.Value, actualRoot, proof.Siblings, proof.Index)
		c.Assert(isValid, qt.IsTrue, qt.Commentf("Proof verification failed for node %d", node.index))

		t.Logf("Proof for node %d verified ✓", node.index)
	}

	t.Log("All compatibility checks passed! ✓")

	// Cleanup
	if ref.tree != nil {
		_ = ref.tree.Close()
	}
}

// TestSolidityLeafPacking verifies the leaf packing formula matches Solidity's implementation.
func TestSolidityLeafPacking(t *testing.T) {
	c := qt.New(t)
	c.Parallel()

	testCases := []struct {
		address      string
		weight       uint64
		expectedLeaf string
	}{
		{
			address:      "0x0000000000000000000000000000000000000001",
			weight:       1,
			expectedLeaf: "309485009821345068724781057", // (1 << 88) | 1
		},
		{
			address:      "0x0000000000000000000000000000000000000002",
			weight:       1,
			expectedLeaf: "618970019642690137449562113", // (2 << 88) | 1
		},
		{
			address:      "0x0000000000000000000000000000000000000002",
			weight:       2,
			expectedLeaf: "618970019642690137449562114", // (2 << 88) | 2
		},
		{
			address:      "0x0000000000000000000000000000000000000003",
			weight:       1,
			expectedLeaf: "928455029464035206174343169", // (3 << 88) | 1
		},
	}

	for i, tc := range testCases {
		addr := common.HexToAddress(tc.address)

		// Calculate packed leaf: (address << 88) | weight
		addrBig := new(big.Int).SetBytes(addr.Bytes())
		weightBig := new(big.Int).SetUint64(tc.weight)

		packedLeaf := new(big.Int).Lsh(addrBig, 88)
		packedLeaf.Or(packedLeaf, weightBig)

		expectedLeafBig := new(big.Int)
		expectedLeafBig.SetString(tc.expectedLeaf, 10)

		c.Assert(packedLeaf.String(), qt.Equals, expectedLeafBig.String(),
			qt.Commentf("Test case %d: address=%s, weight=%d", i, tc.address, tc.weight))

		c.Logf("Test case %d: address=%s, weight=%d, leaf=%s ✓",
			i, tc.address, tc.weight, packedLeaf.String())
	}
}

// TestSolidityRootCalculation verifies that inserting nodes produces the expected roots
// at each step, matching the Solidity contract's behavior.
func TestSolidityRootCalculation(t *testing.T) {
	c := qt.New(t)
	c.Parallel()

	// Test data from Solidity testData.json - BasicOperations scenario
	type step struct {
		address      string
		weight       uint64
		expectedRoot string
		description  string
	}

	steps := []step{
		{
			address:      "0x0000000000000000000000000000000000000002",
			weight:       1,
			expectedRoot: "618970019642690137449562113",
			description:  "First insertion - Bob gets weight 1",
		},
		{
			address:      "0x0000000000000000000000000000000000000003",
			weight:       1,
			expectedRoot: "8161107922390560826582004614572049481782314150751446169603744326598204661278",
			description:  "Second insertion - Charlie gets weight 1",
		},
	}

	censusDB := NewCensusDB(newDatabase(t))
	ref, err := censusDB.New(uuid.New())
	c.Assert(err, qt.IsNil)

	for i, step := range steps {
		addr := common.HexToAddress(step.address)
		weightBytes := make([]byte, 8)
		new(big.Int).SetUint64(step.weight).FillBytes(weightBytes)

		err := ref.Insert(addr.Bytes(), weightBytes)
		c.Assert(err, qt.IsNil, qt.Commentf("Step %d: %s", i, step.description))

		actualRoot := ref.Root()
		actualRootBig := new(big.Int).SetBytes(actualRoot)
		expectedRootBig := new(big.Int)
		expectedRootBig.SetString(step.expectedRoot, 10)

		c.Assert(actualRootBig.String(), qt.Equals, expectedRootBig.String(),
			qt.Commentf("Step %d: %s - Root mismatch", i, step.description))

		t.Logf("Step %d: %s - Root: %s ✓", i, step.description, actualRootBig.String())
	}

	// Cleanup
	if ref.tree != nil {
		_ = ref.tree.Close()
	}
}
