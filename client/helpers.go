package client

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

// RandomBallotFields helper function returns a random slice of types.BigInt
// fields based on the BallotMode configuration provided.
func RandomBallotFields(bm spec.BallotMode) []*types.BigInt {
	fields := make([]*types.BigInt, 0, params.FieldsPerBallot)
	for range params.FieldsPerBallot {
		fields = append(fields, types.NewInt(0))
	}
	stored := map[string]bool{}
	// rand.Int requires a strictly positive max; treat [MinValue, MaxValue] as an inclusive range.
	span := bm.MaxValue - bm.MinValue + 1
	if bm.UniqueValues && uint64(bm.NumFields) > span {
		panic(fmt.Errorf("ballotMode requires %d unique values but range only has %d", bm.NumFields, span))
	}
	maxRandValue := new(big.Int).SetUint64(span)
	valuePadding := new(big.Int).SetUint64(bm.MinValue)
	for i := range bm.NumFields {
		for {
			// generate random field
			field, err := rand.Int(rand.Reader, maxRandValue)
			if err != nil {
				panic(err)
			}
			field.Add(field, valuePadding)
			// if it should be unique and it's already stored, skip it,
			// otherwise add it to the list of fields and continue
			if !bm.UniqueValues || !stored[field.String()] {
				fields[i] = fields[i].SetBigInt(field)
				stored[field.String()] = true
				break
			}
		}
	}
	return fields
}

// WaitUntilCondition helper blocks the execution until a condition is met.
// It checks the condition in a loop with a ticker defined with the provided
// interval until the condition is met or the context provided is done.
func WaitUntilCondition(ctx context.Context, interval time.Duration, condition func() (bool, error)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for condition")
		case <-ticker.C:
			stop, err := condition()
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
		}
	}
}
