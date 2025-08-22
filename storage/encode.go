package storage

import (
	"encoding/json"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/vocdoni/davinci-node/log"
)

// ArtifactEncoding defines the encoding formats for artifacts. There are two
// supported formats: ArtifactEncodingCBOR and ArtifactEncodingJSON.
type ArtifactEncoding int

const (
	// ArtifactEncodingCBOR is the CBOR encoding format.
	ArtifactEncodingCBOR ArtifactEncoding = iota
	// ArtifactEncodingJSON is the JSON encoding format.
	ArtifactEncodingJSON
)

// EncodeArtifact encodes an artifact into the specified encoding format. If no
// format is specified, CBOR is used by default. If a format is specified, it
// will be used instead, only if it is supported.
func EncodeArtifact(a any, encoding ...ArtifactEncoding) ([]byte, error) {
	if len(encoding) > 0 {
		switch encoding[0] {
		case ArtifactEncodingCBOR:
			return EncodeArtifactCBOR(a)
		case ArtifactEncodingJSON:
			res, err := EncodeArtifactJSON(a)
			// if JSON encoding fails, fall back to CBOR
			if err != nil {
				log.Warnw("falling back to CBOR encoding due to JSON encoding failure", "error", err)
				return EncodeArtifactCBOR(a)
			}
			return res, nil
		default:
			return nil, fmt.Errorf("unknown artifact encoding: %d", encoding)
		}
	}
	return EncodeArtifactCBOR(a)
}

// DecodeArtifact decodes an artifact from the specified format. If no format
// is specified, CBOR is used by default. If a format is specified, it will
// be used instead, only if it is supported.
func DecodeArtifact(data []byte, out any, encoding ...ArtifactEncoding) error {
	if len(encoding) > 0 {
		switch encoding[0] {
		case ArtifactEncodingCBOR:
			return DecodeArtifactCBOR(data, out)
		case ArtifactEncodingJSON:
			if err := DecodeArtifactJSON(data, out); err != nil {
				// if JSON encoding fails, fall back to CBOR
				log.Warnw("falling back to CBOR decoding due to JSON decoding failure", "error", err)
				return DecodeArtifactCBOR(data, out)
			}
			return nil
		default:
			return fmt.Errorf("unknown artifact encoding: %d", encoding)
		}
	}
	return DecodeArtifactCBOR(data, out)
}

// EncodeArtifactCBOR encodes an artifact into CBOR format.
func EncodeArtifactCBOR(a any) ([]byte, error) {
	encOpts := cbor.CoreDetEncOptions()
	em, err := encOpts.EncMode()
	if err != nil {
		return nil, fmt.Errorf("encode artifact: %w", err)
	}
	return em.Marshal(a)
}

// DecodeArtifactCBOR decodes a CBOR-encoded artifact into the provided output
// variable.
func DecodeArtifactCBOR(data []byte, out any) error {
	return cbor.Unmarshal(data, out)
}

// EncodeArtifactJSON encodes an artifact into JSON format.
func EncodeArtifactJSON(a any) ([]byte, error) {
	return json.Marshal(a)
}

// DecodeArtifactJSON decodes a JSON-encoded artifact into the provided output
// variable.
func DecodeArtifactJSON(data []byte, out any) error {
	return json.Unmarshal(data, out)
}
