package circuits

// The circuits package contains the circuits used in the voting system. The
// main goal of these circuits its to provide a verifiable way to prove not
// only a valid vote but also a valid voter, without disclose the identity of
// the voter or the value of the vote.
// To achieve that goal, the circuits are used following these steps:
//   1. The voter ciphers the vote value (proving that this process is correct
//      using the BallotProof) and send it to the sequencer.
//   2. The sequencer converts that proof in faster one verifing the proof
//      (proving that this process is correct using the VoteVerifier).
//   3. The sequencer groups the votes in batches reducing the number of proofs
//      (proving that this process is correct using the Aggregator).
//   4. Finally, the sequencer updates the state with the new votes (proving
// 	    that this process is correct using the StateTransition).
// The circuits are defined in the following way:
//
// +------------+
// |   Ballot   |  BabyJubJub (BN254)  	<- native
// |   Proof    |
// +------------+
//
// +------------+
// |    Vote    |  BLS12-377			<- native
// |  Verifier  |  (BN254 inside) 		<- inner
// +------------+
//
// +------------+  BW6-761				<- native
// | Aggregator |  (BLS12-377 inside)	<- inner
// +------------+
//
// +------------+
// |   State    |  BN254				<- native
// | Transition |  (BW6-761 inside)		<- inner
// +------------+
