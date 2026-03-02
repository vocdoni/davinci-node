package circuits

import (
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/types"
)

// FrontendError function is an in-circuit function to print an error message
// and an error trace, making the circuit fail.
func FrontendError(api frontend.API, msg string, trace error) {
	api.Println("in-circuit error: " + msg)
	api.Println(fmt.Sprintf("%s: %s", msg, trace.Error()))
	api.AssertIsEqual(1, 0)
}

// BigIntArrayToN pads the big.Int array to n elements, if needed, with zeros.
func BigIntArrayToN(arr []*big.Int, n int) []*big.Int {
	bigArr := make([]*big.Int, n)
	for i := range n {
		if i < len(arr) {
			bigArr[i] = arr[i]
		} else {
			bigArr[i] = big.NewInt(0)
		}
	}
	return bigArr
}

// BigIntArrayToNInternal pads the types.BigInt array to n elements, if needed,
// with zeros.
func BigIntArrayToNInternal(arr []*big.Int, n int) []*types.BigInt {
	bigArr := make([]*types.BigInt, n)
	for i := range n {
		if i < len(arr) {
			bigArr[i] = new(types.BigInt).SetBigInt(arr[i])
		} else {
			bigArr[i] = types.NewInt(0)
		}
	}
	return bigArr
}

// BigIntArrayToStringArray converts the big.Int array to a string array.
func BigIntArrayToStringArray(arr []*big.Int, n int) []string {
	strArr := []string{}
	for _, b := range BigIntArrayToN(arr, n) {
		strArr = append(strArr, b.String())
	}
	return strArr
}

// StoreConstraintSystem stores the constraint system in a file.
func StoreConstraintSystem(cs constraint.ConstraintSystem, filepath string) error {
	// persist the constraint system
	csFd, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer func() {
		if err := csFd.Close(); err != nil {
			log.Printf("error closing constraint system file: %v", err)
		}
	}()
	if _, err := cs.WriteTo(csFd); err != nil {
		return err
	}
	log.Printf("constraint system written to %s", filepath)
	return nil
}

// StoreVerificationKey stores the verification key in a file.
func StoreVerificationKey(vkey groth16.VerifyingKey, filepath string) error {
	fd, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer func() {
		if err := fd.Close(); err != nil {
			log.Printf("error closing verification key file: %v", err)
		}
	}()
	if _, err := vkey.WriteRawTo(fd); err != nil {
		return err
	}
	log.Printf("verification key written to %s", filepath)
	return nil
}

// StoreProof stores the proof in a file.
func StoreProof(proof groth16.Proof, filepath string) error {
	// persist the proof
	proofFd, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer func() {
		if err := proofFd.Close(); err != nil {
			log.Printf("error closing proof file: %v", err)
		}
	}()
	if _, err := proof.WriteTo(proofFd); err != nil {
		return err
	}
	log.Printf("proof written to %s", filepath)
	return nil
}

// StoreWitness stores the witness in a file.
func StoreWitness(witness witness.Witness, filepath string) error {
	// persist the witness
	witnessFd, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer func() {
		if err := witnessFd.Close(); err != nil {
			log.Printf("error closing witness file: %v", err)
		}
	}()
	bWitness, err := witness.MarshalBinary()
	if err != nil {
		return err
	}
	if _, err := witnessFd.Write(bWitness); err != nil {
		return err
	}
	return nil
}

// BoolToBigInt returns 1 when b is true or 0 otherwise
func BoolToBigInt(b bool) *big.Int {
	if b {
		return big.NewInt(1)
	}
	return big.NewInt(0)
}
