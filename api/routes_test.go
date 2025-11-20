package api

import (
	"fmt"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/workers"
)

func TestEncodeWithParams(t *testing.T) {
	c := qt.New(t)
	testSeed := "test-seed"
	workerName := "test-worker"
	workerAddr := fmt.Sprintf("0x%x", util.RandomHex(20))

	mainAPIUUID, err := workers.WorkerSeedToUUID(testSeed)
	c.Assert(err, qt.IsNil)

	getJobEndpoint := EndpointWithParam(WorkersEndpoint, SequencerUUIDURLParam, mainAPIUUID.String())
	getJobEndpoint = EndpointWithParam(getJobEndpoint, WorkerNameQueryParam, workerName)
	getJobEndpoint = EndpointWithParam(getJobEndpoint, WorkerAddressQueryParam, workerAddr)

	t.Log(getJobEndpoint)
}
