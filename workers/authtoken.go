package workers

import (
	"bytes"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/types"
)

const (
	timestampFormat   = "2006-01-02T15:04:05.000000000Z07:00" // RFC3339FixedNano
	workerSignMessage = `Authorizing worker in sequencer '%s' at %s`

	signatureLen = 65 // 32 bytes r, 32 bytes s, 1 byte v
	timestampLen = len(timestampFormat)
	tokenLen     = signatureLen + timestampLen // 65 bytes signature + timestamp bytes
)

// WorkersAuthTokenData function prepares the required data to generate a worker
// authtoken for the current sequencer. It takes the timestamp provided and
// encode it as token suffix, but also includes it in the signature message. It
// returns the signature message, the timestamp formatted as a string, and the
// token suffix as a byte slice.
func WorkerAuthTokenData(sequencerAddress common.Address, timestamp time.Time) (string, string, types.HexBytes) {
	t := timestamp.UTC().Format(timestampFormat)
	signMessage := fmt.Sprintf(workerSignMessage, sequencerAddress.String(), t)
	return signMessage, t, TimestampToSufix(timestamp)
}

// TimestampToSufix function encodes a time.Time value into a byte slice that
// can be used as a suffix for the worker authentication token. The timestamp is
// formatted using the RFC3339FixedNano format and converted to a byte slice.
// The resulting byte slice has a fixed length defined by timestampLen.
func TimestampToSufix(t time.Time) types.HexBytes {
	b := make([]byte, timestampLen)
	copy(b, []byte(t.UTC().Format(timestampFormat)))
	return types.HexBytes(b)
}

// DecodeWorkerAuthToken function decodes a worker authentication token into
// its signature and timestamp. If the token is invalid, an error is returned.
// It returns the decoded signature and timestamp.
func DecodeWorkerAuthToken(bToken types.HexBytes) (*ethereum.ECDSASignature, time.Time, error) {
	// check if the token has the right size
	if len(bToken) != tokenLen {
		return nil, time.Time{}, fmt.Errorf("invalid worker token length: %d", len(bToken))
	}
	// split the bSignature and the encoded timestamp
	bSignature, encTimestamp := bToken[:signatureLen], bToken[signatureLen:]
	if len(encTimestamp) != timestampLen {
		return nil, time.Time{}, fmt.Errorf("invalid worker token timestamp length: %d", len(encTimestamp))
	}
	// decode the timestamp
	timestamp, err := time.Parse(timestampFormat, string(bytes.TrimRight(encTimestamp, "\x00")))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse worker token timestamp: %w", err)
	}
	// decode the signature
	signature, err := ethereum.BytesToSignature(bSignature)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse worker token signature: %w", err)
	}
	return signature, timestamp, nil
}

// EncodeWorkerAuthToken function encodes a worker authentication token from
// the provided signature and timestamp. It concatenates the signature bytes
// with the timestamp formatted as a byte slice. If the signature is nil or has
// an invalid length, an error is returned. It returns the encoded token as a
// byte slice.
func EncodeWorkerAuthToken(signature *ethereum.ECDSASignature, timestamp time.Time) (types.HexBytes, error) {
	if signature == nil {
		return nil, fmt.Errorf("signature cannot be nil")
	}
	bSignature := signature.Bytes()
	if len(bSignature) != signatureLen {
		return nil, fmt.Errorf("invalid signature length: %d", len(bSignature))
	}
	tokenSuffix := TimestampToSufix(timestamp)
	// concatenate the signature and the token suffix
	token := make([]byte, tokenLen)
	copy(token, bSignature)
	copy(token[signatureLen:], tokenSuffix)
	return types.HexBytes(token), nil
}

// EncodeWorkerAuthTokenFromStringTime function is a wrapper to EncodeWorkerAuthToken
// that accepts a string representation of the timestamp. It parses the string to a
// time.Time value and calls the main function. If the string cannot be parsed, an
// error is returned.
func EncodeWorkerAuthTokenFromStringTime(signature *ethereum.ECDSASignature, strTimestamp string) (types.HexBytes, error) {
	timestamp, err := time.Parse(timestampFormat, strTimestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse worker token timestamp: %w", err)
	}
	return EncodeWorkerAuthToken(signature, timestamp)
}

// VerifyWorkerToken function verifies a worker authentication token against the
// worker's address and the sequencer's address. It decodes the token to get the
// signature and timestamp, then calculates the expected signature message with
// the sequencer address and timestamp. Then validates the signature against
// the worker address using the calculated message. It returns a boolean that
// indicates if the signature is valid or not and the timestamp. If something
// fails, returns an error.
func VerifyWorkerToken(bToken types.HexBytes, workerAddr, seqAddr common.Address) (bool, time.Time, error) {
	signature, timestamp, err := DecodeWorkerAuthToken(bToken)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("failed to decode worker token: %w", err)
	}
	signMessage, _, _ := WorkerAuthTokenData(seqAddr, timestamp)
	valid, _ := signature.Verify([]byte(signMessage), workerAddr)
	return valid, timestamp, nil
}

// VerifyWorkerHexToken function is a wrapper to VerifyWorkerToken that accepts
// a hex-encoded token and a string representation of the worker address.
func VerifyWorkerHexToken(hexToken, strWorkerAddr string, seqAddr common.Address) (bool, time.Time, error) {
	workerAddr := common.HexToAddress(strWorkerAddr)
	token, err := types.HexStringToHexBytes(hexToken)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("failed to decode worker token: %w", err)
	}
	return VerifyWorkerToken(token, workerAddr, seqAddr)
}
