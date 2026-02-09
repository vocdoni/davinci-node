package hash

// ZeroBallotHashHex (a.k.a ZERO_BALLOT_HASH) is the Poseidon hash of 8 fields where each
// field is the 4-tuple (0, 1, 0, 1) (babyjubjub identity points):
//
//	zeroBallotValues = [
//	 0,1,0,1,  0,1,0,1,  0,1,0,1,  0,1,0,1,
//	 0,1,0,1,  0,1,0,1,  0,1,0,1,  0,1,0,1
//	]
const ZeroBallotHashHex = "2c66ee3d8ff0f86c2251e885d4c207e5162c05d0b458c773106cd5579c58bf36"

// Results leaves are constants derived from ZERO_BALLOT_HASH:
//
//	leafResultsAdd = H_3(KEY_RESULTS_ADD, ZERO_BALLOT_HASH, LEAF_DOMAIN)
//	leafResultsSub = H_3(KEY_RESULTS_SUB, ZERO_BALLOT_HASH, LEAF_DOMAIN)
const (
	LeafResultsAddHex = "1f72c52b6e5dedca4f99ecfa24f2776732431e8d544e14c6f78f5042727c4657"
	LeafResultsSubHex = "2b853c511fba705a6030f80ce83d6ee8cbf4a1273724368728c11682eae4c51a"
)
