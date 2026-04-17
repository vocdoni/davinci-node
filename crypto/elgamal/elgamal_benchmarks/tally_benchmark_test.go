package elgamal_benchmarks

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_iden3"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	eg "github.com/vocdoni/davinci-node/crypto/elgamal"
)

// Benchmark to mainly test weighted voting
func BenchmarkDecryptWorstCaseBJJ(b *testing.B) {
	curve := curves.New(bjj.CurveType)
	publicKey, privateKey, err := eg.GenerateKey(curve)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}

	for _, maxMessage := range []uint64{
		65535,
		1 << 20,
		1 << 24,
		2 << 24, // current finalizer bound
		1 << 28,
		1 << 32,
		1 << 36,
		170000000000,
	} {
		msg := new(big.Int).SetUint64(maxMessage)
		c1, c2, _, err := eg.Encrypt(publicKey, msg)
		if err != nil {
			b.Fatalf("encrypt %d: %v", maxMessage, err)
		}

		b.Run(fmt.Sprintf("max_%d", maxMessage), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, recoveredMsg, err := eg.Decrypt(publicKey, privateKey, c1, c2, maxMessage)
				if err != nil {
					b.Fatalf("decrypt %d: %v", maxMessage, err)
				}
				if recoveredMsg.Uint64() != maxMessage {
					b.Fatalf("decrypt %d: got %d", maxMessage, recoveredMsg.Uint64())
				}
			}
		})
	}
}

type tokenWeightedScenario struct {
	name           string
	voters         uint64
	avgWholeTokens uint64
	keptDecimals   uint8
}

func BenchmarkDecryptWeighted50kBJJ(b *testing.B) {
	curve := curves.New(bjj.CurveType)
	publicKey, privateKey, err := eg.GenerateKey(curve)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}

	scenarios := []tokenWeightedScenario{
		{
			name:           "1_token_avg_keep_0_decimals",
			voters:         50_000,
			avgWholeTokens: 1,
			keptDecimals:   0,
		},
		{
			name:           "1_token_avg_keep_3_decimals",
			voters:         50_000,
			avgWholeTokens: 1,
			keptDecimals:   3,
		},
		{
			name:           "1_token_avg_keep_4_decimals",
			voters:         50_000,
			avgWholeTokens: 1,
			keptDecimals:   4,
		},
		{
			name:           "1_token_avg_keep_5_decimals",
			voters:         50_000,
			avgWholeTokens: 1,
			keptDecimals:   5,
		},
		{
			name:           "1_token_avg_keep_6_decimals",
			voters:         50_000,
			avgWholeTokens: 1,
			keptDecimals:   6,
		},
		{
			name:           "100_tokens_avg_keep_0_decimals",
			voters:         50_000,
			avgWholeTokens: 100,
			keptDecimals:   0,
		},
		{
			name:           "100_tokens_avg_keep_2_decimals",
			voters:         50_000,
			avgWholeTokens: 100,
			keptDecimals:   2,
		},
		{
			name:           "100_tokens_avg_keep_3_decimals",
			voters:         50_000,
			avgWholeTokens: 100,
			keptDecimals:   3,
		},
		{
			name:           "1_token_avg_keep_raw_18_decimals",
			voters:         50_000,
			avgWholeTokens: 1,
			keptDecimals:   18,
		},
	}

	for _, scenario := range scenarios {
		totalWeight, ok := scenario.totalWeightUint64()
		if !ok {
			b.Run(scenario.name, func(b *testing.B) {
				b.Skipf("total weight %s exceeds uint64 max %d", scenario.totalWeightString(), uint64(math.MaxUint64))
			})
			continue
		}

		msg := new(big.Int).SetUint64(totalWeight)
		c1, c2, _, err := eg.Encrypt(publicKey, msg)
		if err != nil {
			b.Fatalf("encrypt %s (%d): %v", scenario.name, totalWeight, err)
		}

		b.Run(fmt.Sprintf("%s_total_%d", scenario.name, totalWeight), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, recoveredMsg, err := eg.Decrypt(publicKey, privateKey, c1, c2, totalWeight)
				if err != nil {
					b.Fatalf("decrypt %s (%d): %v", scenario.name, totalWeight, err)
				}
				if recoveredMsg.Uint64() != totalWeight {
					b.Fatalf("decrypt %s: got %d", scenario.name, recoveredMsg.Uint64())
				}
			}
		})
	}
}

func BenchmarkDecryptWeighted2DecimalsMatrixBJJ(b *testing.B) {
	curve := curves.New(bjj.CurveType)
	publicKey, privateKey, err := eg.GenerateKey(curve)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}

	scenarios := []tokenWeightedScenario{}
	for _, voters := range []uint64{10_000, 25_000, 50_000, 100_000} {
		for _, avgWholeTokens := range []uint64{1, 5, 10, 25, 50, 100} {
			scenarios = append(scenarios, tokenWeightedScenario{
				name:           fmt.Sprintf("%dk_voters_avg_%d_00_tokens", voters/1_000, avgWholeTokens),
				voters:         voters,
				avgWholeTokens: avgWholeTokens,
				keptDecimals:   2,
			})
		}
	}
	for _, voters := range []uint64{250_000, 500_000} {
		for _, avgWholeTokens := range []uint64{1, 5, 10} {
			scenarios = append(scenarios, tokenWeightedScenario{
				name:           fmt.Sprintf("%dk_voters_avg_%d_00_tokens", voters/1_000, avgWholeTokens),
				voters:         voters,
				avgWholeTokens: avgWholeTokens,
				keptDecimals:   2,
			})
		}
	}

	for _, scenario := range scenarios {
		totalWeight, ok := scenario.totalWeightUint64()
		if !ok {
			b.Run(scenario.name, func(b *testing.B) {
				b.Skipf("total weight %s exceeds uint64 max %d", scenario.totalWeightString(), uint64(math.MaxUint64))
			})
			continue
		}

		msg := new(big.Int).SetUint64(totalWeight)
		c1, c2, _, err := eg.Encrypt(publicKey, msg)
		if err != nil {
			b.Fatalf("encrypt %s (%d): %v", scenario.name, totalWeight, err)
		}

		b.Run(fmt.Sprintf("%s_total_%d", scenario.name, totalWeight), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, recoveredMsg, err := eg.Decrypt(publicKey, privateKey, c1, c2, totalWeight)
				if err != nil {
					b.Fatalf("decrypt %s (%d): %v", scenario.name, totalWeight, err)
				}
				if recoveredMsg.Uint64() != totalWeight {
					b.Fatalf("decrypt %s: got %d", scenario.name, recoveredMsg.Uint64())
				}
			}
		})
	}
}

func BenchmarkDecryptWeightedMillionAvg2DecimalsBJJ(b *testing.B) {
	curve := curves.New(bjj.CurveType)
	publicKey, privateKey, err := eg.GenerateKey(curve)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}

	scenarios := []tokenWeightedScenario{}
	for _, voters := range []uint64{1, 5, 10, 25, 50, 100, 250, 500, 1_000, 2_000, 5_000, 10_000} {
		scenarios = append(scenarios, tokenWeightedScenario{
			name:           fmt.Sprintf("%d_voters_avg_1000000_00_tokens", voters),
			voters:         voters,
			avgWholeTokens: 1_000_000,
			keptDecimals:   2,
		})
	}

	for _, scenario := range scenarios {
		totalWeight, ok := scenario.totalWeightUint64()
		if !ok {
			b.Run(scenario.name, func(b *testing.B) {
				b.Skipf("total weight %s exceeds uint64 max %d", scenario.totalWeightString(), uint64(math.MaxUint64))
			})
			continue
		}

		msg := new(big.Int).SetUint64(totalWeight)
		c1, c2, _, err := eg.Encrypt(publicKey, msg)
		if err != nil {
			b.Fatalf("encrypt %s (%d): %v", scenario.name, totalWeight, err)
		}

		b.Run(fmt.Sprintf("%s_total_%d", scenario.name, totalWeight), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, recoveredMsg, err := eg.Decrypt(publicKey, privateKey, c1, c2, totalWeight)
				if err != nil {
					b.Fatalf("decrypt %s (%d): %v", scenario.name, totalWeight, err)
				}
				if recoveredMsg.Uint64() != totalWeight {
					b.Fatalf("decrypt %s: got %d", scenario.name, recoveredMsg.Uint64())
				}
			}
		})
	}
}

func BenchmarkDecryptFullTallyMillionAvg2DecimalsBJJ(b *testing.B) {
	curve := curves.New(bjj.CurveType)
	publicKey, privateKey, err := eg.GenerateKey(curve)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}

	scenarios := []tokenWeightedScenario{
		{
			name:           "1000_voters_avg_1000000_00_tokens",
			voters:         1_000,
			avgWholeTokens: 1_000_000,
			keptDecimals:   2,
		},
		{
			name:           "5000_voters_avg_1000000_00_tokens",
			voters:         5_000,
			avgWholeTokens: 1_000_000,
			keptDecimals:   2,
		},
		{
			name:           "10000_voters_avg_1000000_00_tokens",
			voters:         10_000,
			avgWholeTokens: 1_000_000,
			keptDecimals:   2,
		},
	}

	for _, scenario := range scenarios {
		totalWeight, ok := scenario.totalWeightUint64()
		if !ok {
			b.Run(scenario.name, func(b *testing.B) {
				b.Skipf("total weight %s exceeds uint64 max %d", scenario.totalWeightString(), uint64(math.MaxUint64))
			})
			continue
		}

		msg := new(big.Int).SetUint64(totalWeight)
		c1, c2, _, err := eg.Encrypt(publicKey, msg)
		if err != nil {
			b.Fatalf("encrypt %s (%d): %v", scenario.name, totalWeight, err)
		}

		b.Run(fmt.Sprintf("%s_total_%d", scenario.name, totalWeight), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for range 16 {
					_, recoveredMsg, err := eg.Decrypt(publicKey, privateKey, c1, c2, totalWeight)
					if err != nil {
						b.Fatalf("decrypt %s (%d): %v", scenario.name, totalWeight, err)
					}
					if recoveredMsg.Uint64() != totalWeight {
						b.Fatalf("decrypt %s: got %d", scenario.name, recoveredMsg.Uint64())
					}
				}
			}
		})
	}
}

func (s tokenWeightedScenario) totalWeightUint64() (uint64, bool) {
	totalWeight := new(big.Int).SetUint64(s.voters)
	totalWeight.Mul(totalWeight, new(big.Int).SetUint64(s.avgWholeTokens))
	totalWeight.Mul(totalWeight, pow10BigInt(s.keptDecimals))
	if !totalWeight.IsUint64() {
		return 0, false
	}
	return totalWeight.Uint64(), true
}

func (s tokenWeightedScenario) totalWeightString() string {
	totalWeight := new(big.Int).SetUint64(s.voters)
	totalWeight.Mul(totalWeight, new(big.Int).SetUint64(s.avgWholeTokens))
	totalWeight.Mul(totalWeight, pow10BigInt(s.keptDecimals))
	return totalWeight.String()
}

func pow10BigInt(exp uint8) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), new(big.Int).SetUint64(uint64(exp)), nil)
}
