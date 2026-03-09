//go:build !icicle

package prover

import "testing"

func TestGPUProverPanicsWithoutIcicle(t *testing.T) {
	t.Run("GPUProver", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		_, _ = GPUProver(0, nil, nil, nil)
	})

	t.Run("GPUProverWithWitness", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		_, _ = GPUProverWithWitness(0, nil, nil, nil)
	})
}
