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
	tokenSuffix := make([]byte, timestampLen)
	copy(tokenSuffix, []byte(t))
	return signMessage, t, tokenSuffix
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
