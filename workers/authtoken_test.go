package workers

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
)

func TestAuthtoken(t *testing.T) {
	c := qt.New(t)

	sequencerSigner, err := ethereum.NewSigner()
	c.Assert(err, qt.IsNil)

	workerSigner, err := ethereum.NewSigner()
	c.Assert(err, qt.IsNil)

	signMsg, createdAt, _ := WorkerAuthTokenData(sequencerSigner.Address(), time.Now())

	signature, err := workerSigner.Sign([]byte(signMsg))
	c.Assert(err, qt.IsNil)

	timestamp, err := time.Parse(timestampFormat, createdAt)
	c.Assert(err, qt.IsNil)

	token, err := EncodeWorkerAuthToken(signature, timestamp)
	c.Assert(err, qt.IsNil)

	// verify the token
	ok, _, err := VerifyWorkerToken(token, workerSigner.Address(), sequencerSigner.Address())
	c.Assert(err, qt.IsNil)
	c.Assert(ok, qt.Equals, true)
}
