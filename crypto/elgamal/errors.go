package elgamal

import "fmt"

// ErrInvalidCurveType is returned when a ballot references an unsupported curve type.
var ErrInvalidCurveType = fmt.Errorf("invalid curve type")
