package curves

import (
	"slices"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	bjj_gnark "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	bjj_iden3 "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_iden3"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bn254"
)

// New creates a new instance of a Curve implementation based on the provided
// type string. If the type is not supported, it will panic. The supported
// types are defined in this package via the Curves() function, but you can
// also use the IsValid() function to check if a type is supported.
func New(curveType string) ecc.Point {
	switch curveType {
	case bjj_gnark.CurveType:
		return &bjj_gnark.BJJ{}
	case bn254.CurveType:
		return &bn254.G1{}
	case bjj_iden3.CurveType:
		return &bjj_iden3.BJJ{}
	default:
		panic("unsupported curve type: " + curveType)
	}
}

// Curves returns a list of supported curve types.
func Curves() []string {
	return []string{
		bjj_gnark.CurveType,
		bn254.CurveType,
		bjj_iden3.CurveType,
	}
}

func IsValid(curveType string) bool {
	return slices.Contains(Curves(), curveType)
}
